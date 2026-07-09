// src/solve/objective_internal.h

#pragma once

#include <map>
#include <optional>
#include <vector>

#include "model/atoms.h"
#include "model/daily_load.h"
#include "model/index.h"
#include "model/model.h"
#include "model/streams.h"
#include "ortools/sat/cp_model.h"
#include "score/rules.h"
#include "solve/candidates.h"

namespace arrango {
namespace objective_detail {

namespace sat = operations_research::sat;

// One period of one entity-day. A cell is a *constant* (kTrue when the period is
// blocked, kFalse when nothing can cover it) or a derived boolean var (kVar)
// that is 1 iff some candidate occupies the period. Constants let the gap/run
// arithmetic fold away, keeping the model small.
struct Cell {
  enum State { kFalse, kTrue, kVar } state{ kFalse };
  sat::BoolVar var;
  static Cell False() { return { kFalse, {} }; }
  static Cell True() { return { kTrue, {} }; }
  static Cell Var(sat::BoolVar v) { return { kVar, v }; }
};

// Builds the soft objective (only penalties — hard constraints live in
// solve/constraints.cc). Its methods are defined across four files, one block
// each: objective.cc (setup + per-candidate costs), objective_encode.cc (the
// generic occupancy/gap/run CP encoding), objective_students.cc (student
// streams), objective_teachers.cc (teacher gaps + room changes).
class Builder {
 public:
  Builder(const SchoolModel& m, const ModelIndex& ix, const CandidateSet& cs,
          const std::vector<sat::BoolVar>& x, sat::CpModelBuilder& cp,
          const RuleResolver& rules);
  void Build();

 private:
  uint32_t LessonPriorityOf(const LessonInstance& l) const;
  void PerCandidateCosts();

  // --- generic CP occupancy encoding (objective_encode.cc) ---
  std::vector<Cell> MakeCells(
      uint32_t period_count,
      const std::vector<std::vector<sat::BoolVar>>& covers,
      const std::vector<bool>& blocked);
  // gap_rule picks the single-gap exit (soft weight / gap vars == 0 / none);
  // window_rule the adjacent-pair exit (its default is HARD — today's
  // behavior; soft prices pairs, off ignores them). `capped` = teacher gaps.
  void AddGapTerm(const std::vector<Cell>& cells, int64_t weight, bool capped,
                  const ResolvedRule& gap_rule,
                  const ResolvedRule& window_rule, int64_t window_weight);
  // run_rule: soft prices (runs - 1), hard forbids a second run, off skips.
  void AddRunTerm(const std::vector<Cell>& cells, int64_t weight,
                  const ResolvedRule& run_rule);

  // --- domain penalty terms ---
  void StreamTerms();
  // covers/blocked are the stream's raw per-day grids: needed to rebuild
  // subject-FILTERED cells when a "przedmiot extra" exemption applies.
  void MaxLessonsTerms(const StudentStream& st, int stream_idx,
                       const std::vector<std::vector<Cell>>& day_cells,
                       const std::vector<std::vector<std::vector<sat::BoolVar>>>& covers,
                       const std::vector<std::vector<bool>>& blocked,
                       uint32_t priority);
  void DailyLoadTerms(const StudentStream& st,
                      const std::vector<std::vector<Cell>>& day_cells,
                      uint32_t priority);
  void TeacherTerms();

  // ResolveDailyLoad scans all lessons per atom; every stream of a division
  // shares one rule, so resolve once per division.
  const EffectiveDailyLoad& DailyLoadOf(int division_idx);

  const SchoolModel& m_;
  const ModelIndex& ix_;
  const Weights w_;
  const CandidateSet& cs_;
  const std::vector<sat::BoolVar>& x_;
  sat::CpModelBuilder& cp_;
  const RuleResolver& rules_;
  AtomSet atoms_;
  std::vector<StudentStream> streams_;
  std::vector<std::vector<int>> streams_of_lesson_;
  // Inverted indexes built once so the term builders iterate only the
  // candidates that touch a stream / teacher, instead of rescanning all of
  // them per entity (which was O(entities x candidates) on every sub-solve).
  std::vector<std::vector<int>> candidates_of_stream_;
  std::vector<std::vector<int>> candidates_of_teacher_;
  std::vector<std::optional<EffectiveDailyLoad>> daily_load_cache_;
  sat::LinearExpr objective_;
};

}  // namespace objective_detail
}  // namespace arrango
