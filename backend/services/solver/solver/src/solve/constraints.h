// src/solve/constraints.h

#pragma once

#include <map>
#include <vector>

#include "model/index.h"
#include "model/model.h"
#include "ortools/sat/cp_model.h"
#include "solve/candidates.h"

namespace arrango {

  // EXPLAINER-ONLY switches (solve/explain.cc): when present, the two
  // relaxable constraint families are enforced only while their division's
  // switch is true — CP-SAT assumption cores then name the involved
  // divisions. The normal solve path passes nullptr and is bit-identical.
  struct ExplainGates {
    std::map<Id, operations_research::sat::BoolVar> completeness_of_division;
    std::map<Id, operations_research::sat::BoolVar> bounds_of_division;
  };

  // Adds every HARD feasibility constraint over the candidate decision vars `x`:
  //  1. each lesson is placed exactly once (a locked lesson has one candidate),
  //  2. no teacher, room, or student atom occupies a slot more than once,
  //  3. lessons of one parallel block start in the same (day, period) slot,
  //  4. each division's daily load stays inside its [min, max] bounds,
  //  5. lesson links hold (SAME_DAY / DIFFERENT_DAY / CONSECUTIVE),
  //  6. edge lessons open/close their students' day.
  // Soft penalty terms live in the objective (solve/objective.cc); config-HARD
  // rule encodings are added there too, next to their soft twins.
  void AddHardConstraints(
    const SchoolModel& m, const ModelIndex& ix, const CandidateSet& cs,
    const std::vector<operations_research::sat::BoolVar>& x,
    operations_research::sat::CpModelBuilder& cp,
    const ExplainGates* gates = nullptr);

  // Seeds CP-SAT with each lesson's previous placement as a full-layer hint, so
  // an imported or prior timetable becomes the search's starting incumbent.
  void AddPlacementHints(
    const SchoolModel& m, const ModelIndex& ix, const CandidateSet& cs,
    const std::vector<operations_research::sat::BoolVar>& x,
    operations_research::sat::CpModelBuilder& cp);

}  // namespace arrango
