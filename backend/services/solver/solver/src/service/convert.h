// src/service/convert.h

#pragma once

#include "arrango/v1/schedule.pb.h"
#include "arrango/v1/school.pb.h"
#include "arrango/v1/service.pb.h"
#include "model/model.h"
#include "score/scorer.h"
#include "score/verdict.h"
#include "solve/explain.h"
#include "solve/solver.h"
#include "validate/conflict.h"

namespace arrango {

  SchoolModel ModelFromProto(const v1::SchoolModel& in);
  void ModelToProto(const SchoolModel& in, v1::SchoolModel* out);

  ScheduleSnapshot SnapshotFromProto(const v1::ScheduleSnapshot& in);
  void SnapshotToProto(const ScheduleSnapshot& in, v1::ScheduleSnapshot* out);

  void ValidationToProto(const ValidationReport& in, v1::ValidationReport* out);
  void ScoreToProto(const ScoreReport& in, v1::ScoreReport* out);

  SolverConfig ConfigFromProto(const v1::SolverConfig& in);
  void ConfigToProto(const SolverConfig& in, v1::SolverConfig* out);

  RatingConfig RatingFromProto(const v1::ReportingWeights& in);
  void VerdictToProto(const ScheduleVerdict& in, v1::ScheduleVerdict* out);
  void ProgressToProto(const SolveProgressInfo& in, v1::SolveProgress* out);

  RuleConfig RuleConfigFromProto(const v1::RuleConfig& in);
  void RuleConfigToProto(const RuleConfig& in, v1::RuleConfig* out);
  void CoreToProto(const InfeasibleCore& in, v1::InfeasibleCore* out);

}  // namespace arrango
