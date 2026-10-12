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
  PRIVATE
    ${CMAKE_CURRENT_SOURCE_DIR}/src)
target_link_libraries(ratelimitd_cpp PRIVATE CURL::libcurl)
set_target_properties(ratelimitd_cpp PROPERTIES
  OUTPUT_NAME ratelimitd
  VERSION ${PROJECT_VERSION}
  CXX_EXTENSIONS OFF)

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
    target_include_directories(${t} PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/src)
    add_test(NAME cpp.${t} COMMAND ${t})
  endforeach()
endif()
