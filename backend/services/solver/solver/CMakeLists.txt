cmake_minimum_required(VERSION 3.24)
project(arrango_solver LANGUAGES CXX)

# Must match the prebuilt OR-Tools bundle's NDEBUG setting: abseil's
# swisstable layout differs between debug and release builds, and mixing
# them crashes in flat_hash_map destructors.
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE Release)
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# The solver must link ONLY the OR-Tools bundle's protobuf/abseil universe
# plus the gRPC built against it. System gRPC/protobuf/abseil ODR-crash.
if(NOT DEFINED ARRANGO_ORTOOLS_PREFIX)
  set(ARRANGO_ORTOOLS_PREFIX "$ENV{HOME}/.local/opt/or-tools")
endif()
if(NOT DEFINED ARRANGO_GRPC_PREFIX)
  set(ARRANGO_GRPC_PREFIX "$ENV{HOME}/.local/opt/grpc")
endif()
list(PREPEND CMAKE_PREFIX_PATH "${ARRANGO_ORTOOLS_PREFIX}" "${ARRANGO_GRPC_PREFIX}")
set(CMAKE_BUILD_RPATH "${ARRANGO_ORTOOLS_PREFIX}/lib;${ARRANGO_GRPC_PREFIX}/lib")

find_package(ortools CONFIG REQUIRED)
find_package(Protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)
find_package(GTest REQUIRED)

include(cmake/ArrangoProto.cmake)

enable_testing()
include(GoogleTest)

function(arrango_test name)
  add_executable(${name} tests/${name}.cc)
  target_link_libraries(${name} PRIVATE ${ARGN} GTest::gtest_main)
  gtest_discover_tests(${name} DISCOVERY_TIMEOUT 30)
endfunction()

add_library(arrango_model STATIC src/model/index.cc src/model/streams.cc
  src/model/atoms.cc src/model/eligibility.cc src/model/daily_load.cc
  src/model/structure.cc)
target_include_directories(arrango_model PUBLIC src)

add_library(arrango_demo STATIC src/demo/demo_data.cc
  src/demo/demo_production.cc)
target_link_libraries(arrango_demo PUBLIC arrango_model)

add_library(arrango_validate STATIC src/validate/validator.cc
  src/validate/validator_references.cc src/validate/validator_rooms.cc
  src/validate/validator_occupancy.cc src/validate/validator_dailyload.cc
  src/validate/preflight.cc)
target_link_libraries(arrango_validate PUBLIC arrango_model arrango_score)

add_library(arrango_score STATIC src/score/scorer.cc
  src/score/scorer_students.cc src/score/scorer_teachers.cc
  src/score/scorer_aggregate.cc src/score/metrics.cc src/score/verdict.cc
  src/score/scorer_info.cc src/score/rules.cc)
target_link_libraries(arrango_score PUBLIC arrango_model)

add_library(arrango_candidates STATIC src/solve/candidates.cc)
target_link_libraries(arrango_candidates PUBLIC arrango_model arrango_score)

add_library(arrango_solve STATIC src/solve/solver.cc src/solve/objective.cc
  src/solve/objective_encode.cc src/solve/objective_students.cc
  src/solve/objective_teachers.cc
  src/solve/constraints.cc src/solve/cpsat_runner.cc src/solve/lns.cc
  src/solve/explain.cc
  src/solve/trace.cc)
target_link_libraries(arrango_solve PUBLIC arrango_candidates arrango_score
  arrango_validate ortools::ortools)

add_library(arrango_service STATIC
  src/service/convert_common.cc src/service/convert_model.cc
  src/service/convert_model_to.cc
  src/service/convert_config.cc src/service/convert_report.cc
  src/service/service.cc)
target_link_libraries(arrango_service PUBLIC arrango_proto arrango_demo
  arrango_validate arrango_score arrango_solve)

add_executable(arrango-solverd src/service/main.cc)
target_link_libraries(arrango-solverd PRIVATE arrango_service)

arrango_test(proto_smoke_test arrango_proto)
arrango_test(model_index_test arrango_model)
arrango_test(structure_test arrango_model)
arrango_test(streams_test arrango_model)
arrango_test(daily_load_test arrango_model)
arrango_test(demo_data_test arrango_demo arrango_candidates
  arrango_validate)
arrango_test(capabilities_test arrango_solve arrango_demo arrango_validate)
arrango_test(validator_test arrango_validate)
arrango_test(preflight_test arrango_validate)
arrango_test(scorer_test arrango_score)
arrango_test(metrics_test arrango_score)
arrango_test(verdict_test arrango_score arrango_validate)
arrango_test(rules_test arrango_score)
arrango_test(explain_test arrango_solve)
arrango_test(candidates_test arrango_candidates)
arrango_test(lns_test arrango_solve arrango_validate)
arrango_test(solver_hard_test arrango_solve arrango_demo arrango_validate)
arrango_test(solver_soft_test arrango_solve arrango_demo arrango_validate)
arrango_test(service_test arrango_service)

add_executable(arrango_bench tools/bench.cc)
target_link_libraries(arrango_bench PRIVATE arrango_solve arrango_demo
  arrango_validate)
