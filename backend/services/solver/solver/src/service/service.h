// src/service/service.h

#pragma once

#include <string>

#include "arrango/v1/service.grpc.pb.h"
#include "solve/solver.h"

namespace arrango {

  // Post-solve audit: the outgoing schedule must be hard-valid AND its
  // reported objective must equal an independent rescore. A failure means a
  // solver bug — the service falls back instead of shipping it silently.
  struct AuditVerdict {
    bool ok{ true };
    std::string reason;
  };
  // request_rules must be the SAME rule layer the solve ran under — a hard
  // rule prices nothing in the objective, so rescoring with different rules
  // would false-fail the audit.
  AuditVerdict AuditResult(const SchoolModel& model,
    const RuleConfig& request_rules, const SolveResult& result);

  // gRPC facade over the demo generator, validator, scorer, and CP-SAT
  // solver. Solve streams STARTED, one SOLUTION per improvement (each
  // re-validated and re-scored), and a final DONE update; client context
  // cancellation stops the search and returns the best-so-far result.
  class SolverServiceImpl final : public v1::SolverService::Service {
  public:
    grpc::Status GetDemoSchool(grpc::ServerContext* context,
      const v1::DemoRequest* request,
      v1::SchoolModel* response) override;
    grpc::Status Validate(grpc::ServerContext* context,
      const v1::ValidateRequest* request,
      v1::ValidationReport* response) override;
    grpc::Status Score(grpc::ServerContext* context,
      const v1::ScoreRequest* request,
      v1::ScoreReport* response) override;
    grpc::Status Solve(grpc::ServerContext* context,
      const v1::SolveRequest* request,
      grpc::ServerWriter<v1::SolveUpdate>* writer) override;
  };

}  // namespace arrango
