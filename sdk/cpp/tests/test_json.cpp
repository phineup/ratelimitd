#include <cstdio>
#include <cstdlib>
#include <string>

#include "ratelimitd/detail/json.hpp"

static int failures = 0;
#define CHECK(cond)                                                          \
    do {                                                                     \
        if (!(cond)) {                                                       \
            std::fprintf(stderr, "%s:%d: CHECK failed: %s\n", __FILE__, __LINE__, #cond); \
            ++failures;                                                      \
        }                                                                    \
    } while (0)

using ratelimitd::json::Value;

static bool throws(const std::string& s) {
    try {
        Value::parse(s);
    } catch (const std::exception&) {
        return true;
    }
    return false;
}

int main() {
    auto v = Value::parse(R"({"a":1,"b":[true,null,"x\né😀"],"c":{"d":-2.5e1},"e":9007199254740993})");
    CHECK(v.get_int("a") == 1);
    CHECK(v.find("b")->array().size() == 3);
    CHECK(v.find("b")->array()[0].boolean());
    CHECK(v.find("b")->array()[2].string() == "x\n\xC3\xA9\xF0\x9F\x98\x80");
    CHECK(v.find("c")->get_int("d") == -25);
    CHECK(v.get_int("e") == 9007199254740993LL);  // exact int64, no double rounding
    CHECK(v.get_string("missing", "dflt") == "dflt");

    Value::parse("  [ ]  ");
    Value::parse("{}");

    // Round trip with escaping.
    ratelimitd::json::Object o;
    o["k"] = "q\"\\\n\t\x01";
    o["n"] = 42;
    o["f"] = false;
    auto text = Value(o).dump();
    CHECK(text == R"({"f":false,"k":"q\"\\\n\t\u0001","n":42})");
    CHECK(Value::parse(text).get_string("k") == "q\"\\\n\t\x01");

    CHECK(throws(""));
    CHECK(throws("{"));
    CHECK(throws("[1,]"));
    CHECK(throws("{\"a\" 1}"));
    CHECK(throws("\"unterminated"));
    CHECK(throws("tru"));
    CHECK(throws("1 2"));
    CHECK(throws(std::string(200, '[')));  // depth limit

    if (failures) {
        std::fprintf(stderr, "%d failure(s)\n", failures);
        return 1;
    }
    std::puts("test_json: ok");
    return 0;
}
