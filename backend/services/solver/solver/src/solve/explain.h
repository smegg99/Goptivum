// src/solve/explain.h

#pragma once

#include <optional>
#include <string>
#include <vector>

#include "model/index.h"
#include "model/model.h"
#include "score/rules.h"
#include "solve/solver.h"

namespace arrango {

  // One member of an infeasibility explanation. kind is one of:
  // teacher_availability, division_availability, group_availability,
  // locked_lesson, hard_rule, daily_bounds, placement_completeness, hint.
  // Items name MODEL INPUTS the user can act on, never solver internals.
  struct CoreItem {
    std::string kind;
    std::string rule;        // rule key when kind == "hard_rule"
    Id entity_id{ kNoId };
    std::string entity_name;
    std::vector<Id> lesson_ids;
    std::string message;     // human fragment ("teacher TA's availability")
  };

  // "These inputs cannot be satisfied together."
  struct InfeasibleCore {
    std::vector<CoreItem> items;
    bool minimal{ true };    // false when the budget ran out mid-shrink
    std::string message;     // one sentence over all items
    std::vector<CoreItem> hints;  // heuristic timeout suspects
  };

  // Explains an INFEASIBLE solve by re-solving with assumption switches on
  // the suspect families (availability, locks, completeness, daily bounds)
  // and a deletion loop over the active school-wide hard rules. Runs ONLY
  // after infeasibility is already proven — the normal path pays nothing.
  // Returns nullopt when the budget dies before anything is provable.
  std::optional<InfeasibleCore> ExplainInfeasibility(const SchoolModel& m,
    const ModelIndex& ix, const RuleConfig& request_rules,
    const SolveParams& params, double budget_seconds);

  // Heuristic suspects for a no-solution TIMEOUT: tightest room pools,
  // stream-heaviest divisions, most-loaded teachers. Stats only, no solves.
  std::vector<CoreItem> TimeoutHints(const SchoolModel& m,
    const ModelIndex& ix);

}  // namespace arrango
