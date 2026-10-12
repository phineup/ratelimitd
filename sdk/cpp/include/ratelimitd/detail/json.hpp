// Minimal JSON value, parser and serializer. Internal to the SDK.
#pragma once

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <map>
#include <stdexcept>
#include <string>
#include <variant>
#include <vector>

namespace ratelimitd::json {

class Value;
using Array = std::vector<Value>;
using Object = std::map<std::string, Value>;

class Value {
public:
    using Storage = std::variant<std::nullptr_t, bool, std::int64_t, double, std::string, Array, Object>;

    Value() : v_(nullptr) {}
    Value(std::nullptr_t) : v_(nullptr) {}
    Value(bool b) : v_(b) {}
    Value(std::int64_t i) : v_(i) {}
    Value(int i) : v_(static_cast<std::int64_t>(i)) {}
    Value(double d) : v_(d) {}
    Value(const char* s) : v_(std::string(s)) {}
    Value(std::string s) : v_(std::move(s)) {}
    Value(Array a) : v_(std::move(a)) {}
    Value(Object o) : v_(std::move(o)) {}

    bool is_object() const { return std::holds_alternative<Object>(v_); }
    bool is_array() const { return std::holds_alternative<Array>(v_); }
    bool is_string() const { return std::holds_alternative<std::string>(v_); }
    bool is_bool() const { return std::holds_alternative<bool>(v_); }
    bool is_number() const { return std::holds_alternative<std::int64_t>(v_) || std::holds_alternative<double>(v_); }

    const Object& object() const { return std::get<Object>(v_); }
    const Array& array() const { return std::get<Array>(v_); }
    const std::string& string() const { return std::get<std::string>(v_); }
    bool boolean() const { return std::get<bool>(v_); }
    std::int64_t integer() const {
        if (auto* i = std::get_if<std::int64_t>(&v_)) return *i;
        return static_cast<std::int64_t>(std::get<double>(v_));
    }

    /// Object member or nullptr.
    const Value* find(const std::string& key) const {
        if (!is_object()) return nullptr;
        auto it = object().find(key);
        return it == object().end() ? nullptr : &it->second;
    }

    std::string get_string(const std::string& key, std::string def = "") const {
        auto* v = find(key);
        return v && v->is_string() ? v->string() : def;
    }
    std::int64_t get_int(const std::string& key, std::int64_t def = 0) const {
        auto* v = find(key);
        return v && v->is_number() ? v->integer() : def;
    }
    bool get_bool(const std::string& key, bool def = false) const {
        auto* v = find(key);
        return v && v->is_bool() ? v->boolean() : def;
    }

    void dump(std::string& out) const {
        struct Visitor {
            std::string& out;
            void operator()(std::nullptr_t) const { out += "null"; }
            void operator()(bool b) const { out += b ? "true" : "false"; }
            void operator()(std::int64_t i) const { out += std::to_string(i); }
            void operator()(double d) const {
                char buf[32];
                std::snprintf(buf, sizeof buf, "%.17g", d);
                out += buf;
            }
            void operator()(const std::string& s) const { quote(s, out); }
            void operator()(const Array& a) const {
                out += '[';
                for (std::size_t i = 0; i < a.size(); ++i) {
                    if (i) out += ',';
                    a[i].dump(out);
                }
                out += ']';
            }
            void operator()(const Object& o) const {
                out += '{';
                bool first = true;
                for (const auto& [k, v] : o) {
                    if (!first) out += ',';
                    first = false;
                    quote(k, out);
                    out += ':';
                    v.dump(out);
                }
                out += '}';
            }
        };
        std::visit(Visitor{out}, v_);
    }

    std::string dump() const {
        std::string s;
        dump(s);
        return s;
    }

    static Value parse(const std::string& text);

private:
    static void quote(const std::string& s, std::string& out) {
        out += '"';
        for (unsigned char c : s) {
            switch (c) {
                case '"': out += "\\\""; break;
                case '\\': out += "\\\\"; break;
                case '\n': out += "\\n"; break;
                case '\r': out += "\\r"; break;
                case '\t': out += "\\t"; break;
                default:
                    if (c < 0x20) {
                        char buf[8];
                        std::snprintf(buf, sizeof buf, "\\u%04x", c);
                        out += buf;
                    } else {
                        out += static_cast<char>(c);
                    }
            }
        }
        out += '"';
    }

    Storage v_;
};

class Parser {
public:
    explicit Parser(const std::string& s) : s_(s) {}

    Value parse() {
        Value v = value();
        ws();
        if (i_ != s_.size()) fail("trailing characters");
        return v;
    }

private:
    [[noreturn]] void fail(const char* what) const {
        throw std::runtime_error(std::string("json: ") + what + " at offset " + std::to_string(i_));
    }
    void ws() {
        while (i_ < s_.size() && (s_[i_] == ' ' || s_[i_] == '\t' || s_[i_] == '\n' || s_[i_] == '\r')) ++i_;
    }
    char peek() {
        ws();
        if (i_ >= s_.size()) fail("unexpected end");
        return s_[i_];
    }
    void expect(char c) {
        if (peek() != c) fail("unexpected character");
        ++i_;
    }
    bool literal(const char* lit) {
        std::size_t n = std::char_traits<char>::length(lit);
        if (s_.compare(i_, n, lit) == 0) {
            i_ += n;
            return true;
        }
        return false;
    }

    Value value() {
        if (++depth_ > 64) fail("nesting too deep");
        struct Guard {
            int& d;
            ~Guard() { --d; }
        } guard{depth_};
        switch (peek()) {
            case '{': return object();
            case '[': return array();
            case '"': return Value(string());
            case 't': if (literal("true")) return Value(true); break;
            case 'f': if (literal("false")) return Value(false); break;
            case 'n': if (literal("null")) return Value(); break;
            default: return number();
        }
        fail("invalid literal");
    }

    Value object() {
        expect('{');
        Object o;
        if (peek() == '}') {
            ++i_;
            return Value(std::move(o));
        }
        for (;;) {
            if (peek() != '"') fail("expected object key");
            std::string k = string();
            expect(':');
            o.emplace(std::move(k), value());
            char c = peek();
            ++i_;
            if (c == '}') break;
            if (c != ',') fail("expected , or }");
        }
        return Value(std::move(o));
    }

    Value array() {
        expect('[');
        Array a;
        if (peek() == ']') {
            ++i_;
            return Value(std::move(a));
        }
        for (;;) {
            a.push_back(value());
            char c = peek();
            ++i_;
            if (c == ']') break;
            if (c != ',') fail("expected , or ]");
        }
        return Value(std::move(a));
    }

    static void utf8(unsigned cp, std::string& out) {
        if (cp < 0x80) {
            out += static_cast<char>(cp);
        } else if (cp < 0x800) {
            out += static_cast<char>(0xC0 | (cp >> 6));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        } else if (cp < 0x10000) {
            out += static_cast<char>(0xE0 | (cp >> 12));
            out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        } else {
            out += static_cast<char>(0xF0 | (cp >> 18));
            out += static_cast<char>(0x80 | ((cp >> 12) & 0x3F));
            out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        }
    }

    unsigned hex4() {
        if (i_ + 4 > s_.size()) fail("bad \\u escape");
        unsigned v = 0;
        for (int k = 0; k < 4; ++k) {
            char c = s_[i_++];
            v <<= 4;
            if (c >= '0' && c <= '9') v |= c - '0';
            else if (c >= 'a' && c <= 'f') v |= c - 'a' + 10;
            else if (c >= 'A' && c <= 'F') v |= c - 'A' + 10;
            else fail("bad \\u escape");
        }
        return v;
    }

    std::string string() {
        expect('"');
        std::string out;
        for (;;) {
            if (i_ >= s_.size()) fail("unterminated string");
            char c = s_[i_++];
            if (c == '"') return out;
            if (c != '\\') {
                out += c;
                continue;
            }
            if (i_ >= s_.size()) fail("bad escape");
            switch (char e = s_[i_++]) {
                case '"': out += '"'; break;
                case '\\': out += '\\'; break;
                case '/': out += '/'; break;
                case 'b': out += '\b'; break;
                case 'f': out += '\f'; break;
                case 'n': out += '\n'; break;
                case 'r': out += '\r'; break;
                case 't': out += '\t'; break;
                case 'u': {
                    unsigned cp = hex4();
                    if (cp >= 0xD800 && cp < 0xDC00 && s_.compare(i_, 2, "\\u") == 0) {
                        i_ += 2;
                        unsigned lo = hex4();
                        cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                    }
                    utf8(cp, out);
                    break;
                }
                default: (void)e; fail("bad escape");
            }
        }
    }

    Value number() {
        std::size_t start = i_;
        bool is_float = false;
        if (i_ < s_.size() && s_[i_] == '-') ++i_;
        while (i_ < s_.size()) {
            char c = s_[i_];
            if (c >= '0' && c <= '9') {
                ++i_;
            } else if (c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') {
                is_float = true;
                ++i_;
            } else {
                break;
            }
        }
        if (start == i_) fail("unexpected character");
        std::string tok = s_.substr(start, i_ - start);
        char* end = nullptr;
        if (!is_float) {
            long long v = std::strtoll(tok.c_str(), &end, 10);
            if (end == tok.c_str() + tok.size()) return Value(static_cast<std::int64_t>(v));
        }
        double d = std::strtod(tok.c_str(), &end);
        if (end != tok.c_str() + tok.size()) fail("invalid number");
        return Value(d);
    }

    const std::string& s_;
    std::size_t i_ = 0;
    int depth_ = 0;
};

inline Value Value::parse(const std::string& text) { return Parser(text).parse(); }

}  // namespace ratelimitd::json
