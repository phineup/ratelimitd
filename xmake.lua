-- xmake build for the C++ and Rust client SDKs (the Go server is built with `go build`).
--
--   xmake f --rust=y        # also build the Rust SDK through cargo
--   xmake build
--   xmake test              # C++ tests; RATELIMITD_URL=... enables the real-server ones
--   xmake run ratelimitd_rust   # cargo test for the Rust SDK (with --rust=y)
set_project("ratelimitd")
set_version("0.1.0", {soname = true})
set_xmakever("2.8.5")
set_languages("c++17")
add_rules("mode.debug", "mode.release")

option("rust")
    set_default(false)
    set_showmenu(true)
    set_description("Build the Rust SDK with cargo")
option_end()

option("tests")
    set_default(true)
    set_showmenu(true)
    set_description("Build SDK tests")
option_end()

add_requires("libcurl")

target("ratelimitd")
    set_kind("$(kind)")
    add_files("sdk/cpp/src/client.cpp", "sdk/cpp/src/curl_transport.cpp")
    add_includedirs("sdk/cpp/include", {public = true})
    add_headerfiles("sdk/cpp/include/(ratelimitd/**.hpp)", "sdk/cpp/include/(ratelimitd/**.ipp)")
    add_packages("libcurl")
    if is_plat("windows") then
        add_cxxflags("/W4", {tools = "cl"})
    else
        add_cxxflags("-Wall", "-Wextra", "-Wpedantic")
    end

if has_config("tests") then
    for _, name in ipairs({"test_json", "test_client"}) do
        target(name)
            set_kind("binary")
            set_default(false)
            add_files("sdk/cpp/tests/" .. name .. ".cpp")
            add_deps("ratelimitd")
            add_packages("libcurl")
            add_tests("default")
    end
end

if has_config("rust") then
    -- cargo stays the source of truth for the crate; this target only drives it.
    target("ratelimitd_rust")
        set_kind("phony")
        on_build(function (target)
            local cargo = assert(os.getenv("CARGO") or "cargo")
            local args = {"build", "--manifest-path", path.join(os.projectdir(), "sdk", "rust", "Cargo.toml")}
            if is_mode("release") then table.insert(args, "--release") end
            os.execv(cargo, args, {envs = {CARGO_TARGET_DIR = path.join(target:targetdir(), "cargo")}})
        end)
        on_run(function (target)
            local cargo = assert(os.getenv("CARGO") or "cargo")
            os.execv(cargo, {"test", "--manifest-path", path.join(os.projectdir(), "sdk", "rust", "Cargo.toml")},
                     {envs = {CARGO_TARGET_DIR = path.join(target:targetdir(), "cargo")}})
        end)
end
