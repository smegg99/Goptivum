# Generates C++ protobuf + gRPC sources with the OR-Tools bundle's protoc,
# so generated code always matches the linked protobuf runtime.

set(ARRANGO_PROTO_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../proto")
set(ARRANGO_GEN_DIR "${CMAKE_BINARY_DIR}/gen")
set(ARRANGO_PROTOC "${ARRANGO_ORTOOLS_PREFIX}/bin/protoc")
set(ARRANGO_GRPC_PLUGIN "${ARRANGO_GRPC_PREFIX}/bin/grpc_cpp_plugin")

file(MAKE_DIRECTORY "${ARRANGO_GEN_DIR}")

set(ARRANGO_PROTO_FILES
  arrango/v1/school.proto
  arrango/v1/schedule.proto
  arrango/v1/service.proto
)

set(ARRANGO_PROTO_SOURCES "")
foreach(proto_rel ${ARRANGO_PROTO_FILES})
  get_filename_component(proto_name "${proto_rel}" NAME_WE)
  get_filename_component(proto_dir "${proto_rel}" DIRECTORY)
  set(gen_base "${ARRANGO_GEN_DIR}/${proto_dir}/${proto_name}")
  add_custom_command(
    OUTPUT "${gen_base}.pb.cc" "${gen_base}.pb.h"
    COMMAND "${ARRANGO_PROTOC}"
      -I "${ARRANGO_PROTO_DIR}"
      --cpp_out "${ARRANGO_GEN_DIR}"
      "${ARRANGO_PROTO_DIR}/${proto_rel}"
    DEPENDS "${ARRANGO_PROTO_DIR}/${proto_rel}"
    COMMENT "protoc (cpp) ${proto_rel}"
    VERBATIM
  )
  list(APPEND ARRANGO_PROTO_SOURCES "${gen_base}.pb.cc")
endforeach()

# gRPC codegen for the service definition only.
set(ARRANGO_SERVICE_GEN "${ARRANGO_GEN_DIR}/arrango/v1/service.grpc.pb")
add_custom_command(
  OUTPUT "${ARRANGO_SERVICE_GEN}.cc" "${ARRANGO_SERVICE_GEN}.h"
  COMMAND "${ARRANGO_PROTOC}"
    -I "${ARRANGO_PROTO_DIR}"
    --grpc_out "${ARRANGO_GEN_DIR}"
    "--plugin=protoc-gen-grpc=${ARRANGO_GRPC_PLUGIN}"
    "${ARRANGO_PROTO_DIR}/arrango/v1/service.proto"
  DEPENDS "${ARRANGO_PROTO_DIR}/arrango/v1/service.proto"
  COMMENT "protoc (grpc) arrango/v1/service.proto"
  VERBATIM
)
list(APPEND ARRANGO_PROTO_SOURCES "${ARRANGO_SERVICE_GEN}.cc")

add_library(arrango_proto STATIC ${ARRANGO_PROTO_SOURCES})
target_include_directories(arrango_proto PUBLIC "${ARRANGO_GEN_DIR}")
target_link_libraries(arrango_proto PUBLIC protobuf::libprotobuf gRPC::grpc++)
