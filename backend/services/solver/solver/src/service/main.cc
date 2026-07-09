// src/service/main.cc

#include <cstdio>
#include <string>

#include <grpcpp/grpcpp.h>

#include "service/service.h"

int main(int argc, char** argv) {
  std::string listen = "127.0.0.1:50061";
  for (int i = 1; i < argc; ++i) {
    std::string arg = argv[i];
    if (arg.rfind("--listen=", 0) == 0) {
      listen = arg.substr(9);
    }
    else {
      std::fprintf(stderr, "usage: %s [--listen=host:port]\n", argv[0]);
      return 2;
    }
  }

  arrango::SolverServiceImpl service;
  grpc::ServerBuilder builder;
  // gRPC defaults to SO_REUSEPORT: a second instance would silently share
  // the port and serve stale code. Fail loudly instead.
  builder.AddChannelArgument(GRPC_ARG_ALLOW_REUSEPORT, 0);
  builder.AddListeningPort(listen, grpc::InsecureServerCredentials());
  builder.RegisterService(&service);
  std::unique_ptr<grpc::Server> server = builder.BuildAndStart();
  if (!server) {
    std::fprintf(stderr, "failed to listen on %s\n", listen.c_str());
    return 1;
  }
  std::printf("arrango-solverd listening on %s\n", listen.c_str());
  std::fflush(stdout);
  server->Wait();
  return 0;
}
