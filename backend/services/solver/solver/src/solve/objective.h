// src/solve/objective.h

#pragma once

#include <vector>

#include "model/index.h"
#include "model/model.h"
#include "ortools/sat/cp_model.h"
#include "score/rules.h"
#include "solve/candidates.h"

namespace arrango {

  // Adds the soft-preference objective (Minimize) to the CP-SAT model. Every
  // term mirrors the scorer's semantics through score/penalty_defs.h, so on a
  // hard-valid schedule the objective value equals the scorer's total penalty.
  // Each rule's resolved mode picks the term's exit: soft = weighted cost,
  // hard = its violation variables constrained to zero, off = no term.
  void AddSoftObjective(const SchoolModel& m, const ModelIndex& ix,
    const CandidateSet& cs,
    const std::vector<operations_research::sat::BoolVar>& x,
    operations_research::sat::CpModelBuilder& cp, const RuleResolver& rules);

}  // namespace arrango
