# Common CMake entry point for the C++ and Rust SDKs.
#
#   cmake -S . -B build -DRATELIMITD_BUILD_RUST=ON
#   cmake --build build
#   ctest --test-dir build --output-on-failure
#
# The ratelimitd server itself is a Go program (see the Go toolchain / Dockerfile);
# this file only covers the client SDKs under sdk/.
cmake_minimum_required(VERSION 3.16)

project(ratelimitd
  VERSION 0.1.0
  DESCRIPTION "Client SDKs for the ratelimitd rate limiting sidecar"
  HOMEPAGE_URL "https://github.com/phineup/ratelimitd"
  LANGUAGES CXX)

if(CMAKE_SOURCE_DIR STREQUAL CMAKE_CURRENT_SOURCE_DIR)
  set(_ratelimitd_top_level ON)
else()
  set(_ratelimitd_top_level OFF)
endif()

option(RATELIMITD_BUILD_CPP "Build the C++ SDK" ON)
option(RATELIMITD_BUILD_RUST "Build the Rust SDK (needs cargo)" OFF)
option(RATELIMITD_BUILD_TESTS "Build and register SDK tests" ${_ratelimitd_top_level})
option(RATELIMITD_INSTALL "Generate install rules for the C++ SDK" ${_ratelimitd_top_level})

include(GNUInstallDirs)

if(RATELIMITD_BUILD_CPP)
  set(RATELIMITD_CPP_BUILD_TESTS ${RATELIMITD_BUILD_TESTS})
  add_subdirectory(sdk/cpp)

  if(RATELIMITD_INSTALL)
    include(CMakePackageConfigHelpers)

    install(TARGETS ratelimitd_cpp
      EXPORT ratelimitdTargets
      ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
      LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
      RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
      INCLUDES DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
    install(DIRECTORY sdk/cpp/include/ratelimitd DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
    install(EXPORT ratelimitdTargets
      NAMESPACE ratelimitd::
      FILE ratelimitdTargets.cmake
      DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/ratelimitd)

    # Consumers use: find_package(ratelimitd CONFIG REQUIRED)
    #                target_link_libraries(app PRIVATE ratelimitd::ratelimitd_cpp)
    file(WRITE ${CMAKE_CURRENT_BINARY_DIR}/ratelimitdConfig.cmake.in [=[
@PACKAGE_INIT@
include(CMakeFindDependencyMacro)
find_dependency(CURL)
include("${CMAKE_CURRENT_LIST_DIR}/ratelimitdTargets.cmake")
if(NOT TARGET ratelimitd::cpp)
  add_library(ratelimitd::cpp ALIAS ratelimitd::ratelimitd_cpp)
endif()
check_required_components(ratelimitd)
]=])
    configure_package_config_file(
      ${CMAKE_CURRENT_BINARY_DIR}/ratelimitdConfig.cmake.in
      ${CMAKE_CURRENT_BINARY_DIR}/ratelimitdConfig.cmake
      INSTALL_DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/ratelimitd)
    write_basic_package_version_file(
      ${CMAKE_CURRENT_BINARY_DIR}/ratelimitdConfigVersion.cmake
      VERSION ${PROJECT_VERSION}
      COMPATIBILITY SameMajorVersion)
    install(FILES
      ${CMAKE_CURRENT_BINARY_DIR}/ratelimitdConfig.cmake
      ${CMAKE_CURRENT_BINARY_DIR}/ratelimitdConfigVersion.cmake
      DESTINATION ${CMAKE_INSTALL_LIBDIR}/cmake/ratelimitd)
  endif()
endif()

if(RATELIMITD_BUILD_RUST)
  add_subdirectory(sdk/rust)
endif()

if(RATELIMITD_BUILD_TESTS)
  enable_testing()
endif()
