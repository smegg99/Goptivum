// src/model/eligibility.h

#pragma once

#include <vector>

#include "model/index.h"
#include "model/model.h"

namespace arrango {

  // What to do with rooms whose capacity is unknown when the lesson's
  // participant count is known. The construct/repair phases apply the penalty
  // for kAllowPenalized; eligibility only keeps or drops the room.
  enum class UnknownCapacityPolicy { kAllow, kForbid, kAllowPenalized };

  // Students attending the lesson: sum over participants of the group's
  // student_count (group participant) or the division's student_count
  // (whole-division participant). Returns -1 when any needed count is unknown
  // (count_source == kUnknown) — the caller must treat capacity as unchecked.
  int ParticipantCount(const SchoolModel& m, const ModelIndex& ix,
    const LessonInstance& l);

  struct RoomEligibility {
    std::vector<int> rooms;  // sorted; designator + capacity filtered
    // True when the designator filter left rooms but the capacity filter
    // removed them all — viability (inc 3) reports this distinctly from an
    // empty designator set.
    bool capacity_removed_all{};
  };

  // EligibleRooms (designator allow/disallow) plus a capacity filter. A room
  // with KNOWN capacity is removed when its capacity < the (known) participant
  // count. Rooms with UNKNOWN capacity follow `policy`: kAllow / kAllowPenalized
  // keep them, kForbid drops them (only when the participant count is known —
  // an unknown count leaves every room in). Availability at a time is applied
  // later by the candidate builder.
  RoomEligibility ResolveEligibleRooms(const SchoolModel& m,
    const ModelIndex& ix,
    const LessonInstance& l,
    UnknownCapacityPolicy policy);

}  // namespace arrango
