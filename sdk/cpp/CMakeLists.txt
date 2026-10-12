# C++ SDK. Usable standalone (cmake -S sdk/cpp) or via the repository root CMakeLists.txt.
cmake_minimum_required(VERSION 3.16)

if(NOT DEFINED PROJECT_NAME)
  project(ratelimitd_cpp VERSION 0.1.0 LANGUAGES CXX)
  set(_RATELIMITD_CPP_STANDALONE ON)
endif()

option(RATELIMITD_CPP_BUILD_TESTS "Build the C++ SDK tests" ${_RATELIMITD_CPP_STANDALONE})

find_package(CURL REQUIRED)

add_library(ratelimitd_cpp
  src/client.cpp
  src/curl_transport.cpp)
add_library(ratelimitd::cpp ALIAS ratelimitd_cpp)

target_compile_features(ratelimitd_cpp PUBLIC cxx_std_17)
target_include_directories(ratelimitd_cpp
  PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:${CMAKE_INSTALL_INCLUDEDIR}>
  )
target_link_libraries(ratelimitd_cpp PRIVATE CURL::libcurl)
set_target_properties(ratelimitd_cpp PROPERTIES
  OUTPUT_NAME ratelimitd
  VERSION ${PROJECT_VERSION}
  CXX_EXTENSIONS OFF)

# Header-only flavour: no library to build, the implementation is included by
# <ratelimitd/ratelimitd.hpp>. Consumers still link libcurl (propagated here).
add_library(ratelimitd_cpp_header_only INTERFACE)
add_library(ratelimitd::cpp_header_only ALIAS ratelimitd_cpp_header_only)
target_compile_features(ratelimitd_cpp_header_only INTERFACE cxx_std_17)
target_compile_definitions(ratelimitd_cpp_header_only INTERFACE RATELIMITD_HEADER_ONLY)
target_include_directories(ratelimitd_cpp_header_only INTERFACE
  $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
  $<INSTALL_INTERFACE:${CMAKE_INSTALL_INCLUDEDIR}>)
target_link_libraries(ratelimitd_cpp_header_only INTERFACE CURL::libcurl)

if(MSVC)
  target_compile_options(ratelimitd_cpp PRIVATE /W4)
else()
  target_compile_options(ratelimitd_cpp PRIVATE -Wall -Wextra -Wpedantic)
endif()

if(RATELIMITD_CPP_BUILD_TESTS)
  enable_testing()
  foreach(t test_json test_client)
    add_executable(${t} tests/${t}.cpp)
    target_link_libraries(${t} PRIVATE ratelimitd_cpp)
    add_test(NAME cpp.${t} COMMAND ${t})
  endforeach()

  # The same client tests against the header-only flavour.
  add_executable(test_client_header_only tests/test_client.cpp)
  target_link_libraries(test_client_header_only PRIVATE ratelimitd_cpp_header_only)
  add_test(NAME cpp.test_client_header_only COMMAND test_client_header_only)
endif()
