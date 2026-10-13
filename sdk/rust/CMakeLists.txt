# Rust SDK glue for CMake: builds the crate with cargo and runs `cargo test` under CTest.
# cargo stays the source of truth for the crate; this only makes it part of a CMake build.
cmake_minimum_required(VERSION 3.16)

find_program(RATELIMITD_CARGO cargo)
if(NOT RATELIMITD_CARGO)
  message(FATAL_ERROR "cargo not found; install Rust (https://rustup.rs) or set -DRATELIMITD_BUILD_RUST=OFF")
endif()

set(_manifest "${CMAKE_CURRENT_LIST_DIR}/Cargo.toml")
set(_target_dir "${CMAKE_CURRENT_BINARY_DIR}/cargo-target")
set(_profile_args "")
if(CMAKE_BUILD_TYPE STREQUAL "Release" OR CMAKE_BUILD_TYPE STREQUAL "RelWithDebInfo" OR CMAKE_BUILD_TYPE STREQUAL "MinSizeRel")
  set(_profile_args --release)
endif()

add_custom_target(ratelimitd_rust ALL
  COMMAND ${CMAKE_COMMAND} -E env CARGO_TARGET_DIR=${_target_dir}
          ${RATELIMITD_CARGO} build ${_profile_args} --manifest-path ${_manifest}
  COMMENT "Building the ratelimitd Rust SDK with cargo"
  USES_TERMINAL)

if(RATELIMITD_BUILD_TESTS)
  enable_testing()
  add_test(NAME rust.cargo_test
    COMMAND ${CMAKE_COMMAND} -E env CARGO_TARGET_DIR=${_target_dir}
            ${RATELIMITD_CARGO} test ${_profile_args} --manifest-path ${_manifest})
endif()
