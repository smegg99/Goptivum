// src/solve/candidates.h

#pragma once

#include <string>
#include <variant>
#include <vector>

#include "model/eligibility.h"
#include "model/index.h"
#include "model/model.h"
#include "score/rules.h"

namespace arrango {

  // One potential placement of a lesson: x[lesson, day, start, room].
  struct Candidate {
    int lesson_idx{};
    int day_idx{};
    uint32_t start{};
    int room_idx{ -1 };  // -1 = lesson takes no room
  };

  struct CandidateSet {
    std::vector<Candidate> all;
    // Candidate indices (into `all`) per lesson index; same order as lessons.
    std::vector<std::vector<int>> by_lesson;
  };

  // Enumerates pruned candidates: day bounds, room eligibility (designator +
  // capacity under `policy`), locked placements, external blocks and
  // teacher/room unavailability are all applied before any CP-SAT variable
  // exists. Returns an error message naming the first lesson whose candidate
  // set is empty (=> infeasible).
  //
  // `max_candidates_per_lesson` (0 = unlimited) caps the ROOM dimension: a
  // lesson with far more eligible rooms than time slots keeps only a rotated
  // window of them (offset per lesson so concurrent lessons spread across the
  // room set, always keeping any previous placement's room). The kept rooms are
  // real eligible rooms, so every candidate stays a valid placement — this only
  // trims interchangeable-room choice to keep the model tractable.
  //
  // `rules` (optional): a lesson whose late_student rule is HARD for any
  // participant division — or whose teacher's late_teacher rule is HARD —
  // gets its slots past the late threshold pruned here, for maximal CP
  // propagation. A lesson that only fits late is named as infeasible.
  std::variant<CandidateSet, std::string> BuildCandidates(
    const SchoolModel& m, const ModelIndex& ix,
    UnknownCapacityPolicy capacity_policy = UnknownCapacityPolicy::kAllow,
    uint32_t max_candidates_per_lesson = 0,
    const RuleResolver* rules = nullptr);

  // Cheap upper bound on the UNCAPPED BuildCandidates output size: per lesson,
  // day-fitting starts times eligible rooms (1 for locked/roomless lessons).
  // External blocks are ignored, so this never under-counts. Lets the solver
  // decide on a room cap without materializing millions of candidates first.
  uint64_t EstimateCandidateCount(const SchoolModel& m, const ModelIndex& ix,
    UnknownCapacityPolicy capacity_policy);

}  // namespace arrango
