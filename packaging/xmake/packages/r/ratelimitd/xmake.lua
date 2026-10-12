-- xmake package repository for ratelimitd's C++ SDK (optionally with the Rust SDK).
--
-- Use straight from a checkout (no network, builds the working tree):
--
--   add_repositories("ratelimitd /path/to/ratelimitd/packaging/xmake")
--   add_requires("ratelimitd")
--   target("app")
--       set_kind("binary")
--       add_files("main.cpp")
--       add_packages("ratelimitd")
--
-- Or from GitHub (always the repository's default packaging branch):
--
--   add_repositories("ratelimitd https://github.com/phineup/ratelimitd.git packaging/xmake")
--
-- Rust SDK: it is a regular crate, so use it with cargo (`ratelimitd = "0.1"`); set
-- `add_requires("ratelimitd", {configs = {rust = true}})` to additionally build and test it
-- through the repository's CMake glue during package installation.
package("ratelimitd")
    set_homepage("https://github.com/phineup/ratelimitd")
    set_description("Client SDK for the ratelimitd rate limiting sidecar")
    set_license("MIT")

    -- Installed from the repository checkout this package file lives in.
    -- For a tagged release replace this with:
    --   add_urls("https://github.com/phineup/ratelimitd/archive/refs/tags/$(version).tar.gz",
    --            "https://github.com/phineup/ratelimitd.git")
    --   add_versions("v0.1.0", "<sha256 of the tag tarball>")
    set_sourcedir(path.join(os.scriptdir(), "..", "..", "..", "..", ".."))

    add_configs("rust", {description = "Also build and test the Rust SDK (needs cargo).", default = false, type = "boolean"})

    add_deps("cmake")
    add_deps("libcurl")

    on_load(function (package)
        if package:config("rust") then
            package:add("deps", "rust", {kind = "binary", private = true})
        end
    end)

    on_install("linux", "macosx", "bsd", "mingw", "windows", function (package)
        local configs = {
            "-DRATELIMITD_BUILD_TESTS=OFF",
            "-DRATELIMITD_BUILD_CPP=ON",
            "-DRATELIMITD_INSTALL=ON",
            "-DRATELIMITD_BUILD_RUST=" .. (package:config("rust") and "ON" or "OFF"),
            "-DCMAKE_BUILD_TYPE=" .. (package:is_debug() and "Debug" or "Release"),
            "-DBUILD_SHARED_LIBS=" .. (package:config("shared") and "ON" or "OFF"),
        }
        import("package.tools.cmake").install(package, configs)
    end)

    on_test(function (package)
        assert(package:check_cxxsnippets({test = [[
            #include <ratelimitd/ratelimitd.hpp>
            void test() {
                ratelimitd::Client client("http://127.0.0.1:8080");
                auto req = ratelimitd::CheckRequest::for_policy("user", "login");
                (void)client; (void)req;
            }
        ]]}, {configs = {languages = "c++17"}}))
    end)
