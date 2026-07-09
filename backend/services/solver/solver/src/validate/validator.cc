// src/validate/validator.cc

#include "validate/validator_internal.h"

namespace arrango {
namespace validating {

Checker::Checker(const SchoolModel& m, const ScheduleSnapshot& s)
    : m_(m), s_(s), ix_(ModelIndex::Build(m)) {}

ValidationReport Checker::Run() {
  CheckModelReferences();
  CollectPlacements();
  CheckMissingAndDuplicates();
  CheckRooms();
  CheckOccupancy();
  CheckParallelBlocks();
  CheckLessonLinks();
  CheckEdgePlacement();
  CheckLocked();
  CheckExternalBlocks();
  CheckDailyLoads();
  report_.valid = report_.conflicts.empty();
  return std::move(report_);
}

// Structured lesson reference for a lesson id, using its placement in the
// snapshot when it is placed.
LessonRef Checker::LessonRefFor(Id lesson_id) const {
  int li = ix_.LessonIdx(lesson_id);
  uint32_t duration = li >= 0 ? m_.lessons[li].duration : 1;
  auto it = placement_of_.find(lesson_id);
  if (it == placement_of_.end()) {
    return { lesson_id, kNoId, 0, duration, kNoId };
  }
  return { lesson_id, it->second.day_id, it->second.start_period, duration,
          it->second.room_id };
}

// Adds a conflict and auto-builds its structured locus: a typed entity ref
// (when entity_kind is given), a lesson ref per conflicting lesson (with
// placement), and a single-period time span. Sites that need richer spans or
// extra entities pass an `extra` locus that is merged in.
void Checker::Add(ConflictKind kind, std::string message,
                  std::vector<Id> lessons, Id entity, Id day, uint32_t period,
                  EntityKind entity_kind, IssueLocus extra) {
  Conflict c;
  c.kind = kind;
  c.message = std::move(message);
  c.lesson_ids = lessons;
  c.entity_id = entity;
  c.day_id = day;
  c.period = period;
  if (entity_kind != EntityKind::kUnspecified && entity != kNoId) {
    c.locus.entities.push_back({ entity_kind, entity });
  }
  for (Id lid : lessons) c.locus.lessons.push_back(LessonRefFor(lid));
  if (day != kNoId && extra.spans.empty()) {
    c.locus.spans.push_back({ day, period, 1 });
  }
  for (auto& e : extra.entities) c.locus.entities.push_back(e);
  for (auto& l : extra.lessons) c.locus.lessons.push_back(l);
  for (auto& s : extra.spans) c.locus.spans.push_back(s);
  report_.conflicts.push_back(std::move(c));
}

}  // namespace validating

ValidationReport Validate(const SchoolModel& model,
                          const ScheduleSnapshot& snapshot) {
  return validating::Checker(model, snapshot).Run();
}

}  // namespace arrango
