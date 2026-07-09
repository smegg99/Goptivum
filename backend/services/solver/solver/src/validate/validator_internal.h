// src/validate/validator_internal.h

#pragma once

#include <string>
#include <unordered_map>
#include <vector>

#include "model/index.h"
#include "model/model.h"
#include "validate/validator.h"

namespace arrango {
namespace validating {

// One placed lesson resolved to indices.
struct Placed {
  int lesson_idx;
  int day_idx;
  uint32_t start;
  uint32_t duration;
  int room_idx;  // -1 = none
};

// Independently checks a schedule against every hard rule. Its methods are
// defined across validator.cc (setup + helpers), validator_references.cc,
// validator_rooms.cc, validator_occupancy.cc and validator_dailyload.cc — one
// rule group per file. Never relies on the solver model for correctness.
class Checker {
 public:
  Checker(const SchoolModel& m, const ScheduleSnapshot& s);
  ValidationReport Run();

 private:
  LessonRef LessonRefFor(Id lesson_id) const;
  void Add(ConflictKind kind, std::string message, std::vector<Id> lessons,
           Id entity = kNoId, Id day = kNoId, uint32_t period = 0,
           EntityKind entity_kind = EntityKind::kUnspecified,
           IssueLocus extra = {});

  void CheckModelReferences();
  void CollectPlacements();
  void CheckMissingAndDuplicates();
  void CheckRooms();
  void CheckOccupancy();
  void CheckParallelBlocks();
  void CheckLessonLinks();
  void CheckEdgePlacement();
  void CheckLocked();
  void CheckExternalBlocks();
  void CheckDailyLoads();

  const SchoolModel& m_;
  const ScheduleSnapshot& s_;
  ModelIndex ix_;
  ValidationReport report_;
  std::vector<Placed> placed_;
  std::unordered_map<int, int> seen_count_;
  std::unordered_map<Id, Placement> placement_of_;  // lesson id -> placement
};

}  // namespace validating
}  // namespace arrango
