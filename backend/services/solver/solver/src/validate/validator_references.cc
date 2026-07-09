// src/validate/validator_references.cc

#include <map>
#include <string>
#include <vector>

#include "validate/validator_internal.h"

namespace arrango {
namespace validating {

void Checker::CheckModelReferences() {
  for (const Division& c : m_.divisions) {
    if (ix_.YearIdx(c.year_id) < 0) {
      Add(ConflictKind::kInvalidReference,
        "division " + c.name + " references unknown year", {}, c.id);
    }
  }
  for (const Group& g : m_.groups) {
    if (ix_.DivisionIdx(g.division_id) < 0) {
      Add(ConflictKind::kInvalidReference,
        "group " + g.name + " references unknown division", {}, g.id);
    }
    // A bad split reference silently falls back to "private open split"
    // in the index, so it must be reported here.
    if (g.split_id != kNoId) {
      int s = ix_.SplitIdx(g.split_id);
      if (s < 0) {
        Add(ConflictKind::kInvalidReference,
          "group " + g.name + " references unknown split", {}, g.id);
      }
      else if (m_.splits[s].division_id != g.division_id) {
        Add(ConflictKind::kInvalidReference,
          "group " + g.name + " references split '" + m_.splits[s].name +
          "' of another division", {}, g.id);
      }
    }
  }
  for (const Split& s : m_.splits) {
    if (ix_.DivisionIdx(s.division_id) < 0) {
      Add(ConflictKind::kInvalidReference,
        "split " + s.name + " references unknown division", {}, s.id);
    }
  }
  {
    // Duplicate designators make eligibility ambiguous.
    std::map<std::string, std::vector<Id>> by_designator;
    for (const Room& r : m_.rooms) {
      if (!r.designator.empty()) by_designator[r.designator].push_back(r.id);
    }
    for (const auto& [designator, ids] : by_designator) {
      if (ids.size() > 1) {
        IssueLocus extra;
        for (Id rid : ids) extra.entities.push_back({ EntityKind::kRoom, rid });
        Add(ConflictKind::kDuplicateDesignator,
          "rooms share designator '" + designator + "'", {}, ids.front(),
          kNoId, 0, EntityKind::kUnspecified, std::move(extra));
      }
    }
  }
  for (const LessonInstance& l : m_.lessons) {
    if (l.duration < 1) {
      Add(ConflictKind::kInvalidDuration, "lesson has zero duration", { l.id });
    }
    if (l.participants.empty()) {
      Add(ConflictKind::kInvalidReference, "lesson has no participants",
        { l.id });
    }
    for (const Participant& p : l.participants) {
      if (ix_.DivisionIdx(p.division_id) < 0) {
        Add(ConflictKind::kInvalidReference,
          "lesson references unknown division", { l.id });
      }
      if (p.group_id != kNoId) {
        int g = ix_.GroupIdx(p.group_id);
        if (g < 0) {
          Add(ConflictKind::kInvalidReference,
            "lesson references unknown group", { l.id });
        }
        else if (m_.groups[g].division_id != p.division_id) {
          Add(ConflictKind::kInvalidReference,
            "lesson group belongs to a different division", { l.id });
        }
      }
    }
    if (ix_.SubjectIdx(l.subject_id) < 0) {
      Add(ConflictKind::kInvalidReference,
        "lesson references unknown subject", { l.id });
    }
    if (l.requires_teacher && ix_.TeacherIdx(l.teacher_id) < 0) {
      Add(ConflictKind::kInvalidReference,
        "lesson requires a teacher but references none/unknown", { l.id });
    }
    auto check_designators = [&] (const std::vector<std::string>& list,
      const char* what) {
        for (const std::string& d : list) {
          bool known = false;
          for (const Room& r : m_.rooms) known |= r.designator == d;
          if (!known) {
            Add(ConflictKind::kInvalidReference,
              std::string("lesson ") + what + " unknown designator '" + d + "'",
              { l.id });
          }
        }
      };
    check_designators(l.allowed_room_designators, "allows");
    check_designators(l.disallowed_room_designators, "disallows");
    if (l.fixed_room_id != kNoId && ix_.RoomIdx(l.fixed_room_id) < 0) {
      Add(ConflictKind::kInvalidReference,
        "lesson fixes an unknown room", { l.id }, l.fixed_room_id);
    }
  }
}

void Checker::CollectPlacements() {
  for (const ScheduledLesson& sl : s_.lessons) {
    int li = ix_.LessonIdx(sl.lesson_id);
    if (li < 0) {
      Add(ConflictKind::kInvalidReference,
        "snapshot schedules unknown lesson", { sl.lesson_id });
      continue;
    }
    seen_count_[li]++;
    if (seen_count_[li] > 1) continue;  // duplicate handled separately
    const LessonInstance& l = m_.lessons[li];
    int di = ix_.DayIdx(sl.placement.day_id);
    if (di < 0) {
      Add(ConflictKind::kInvalidReference,
        "scheduled lesson references unknown day", { l.id },
        sl.placement.day_id);
      continue;
    }
    if (sl.placement.start_period + l.duration > m_.days[di].period_count) {
      Add(ConflictKind::kOutOfBounds, "lesson does not fit inside the day",
        { l.id }, kNoId, sl.placement.day_id, sl.placement.start_period);
      continue;
    }
    int ri = -1;
    if (sl.placement.room_id != kNoId) {
      ri = ix_.RoomIdx(sl.placement.room_id);
      if (ri < 0) {
        Add(ConflictKind::kInvalidReference,
          "scheduled lesson references unknown room", { l.id },
          sl.placement.room_id);
        continue;
      }
    }
    placed_.push_back({ li, di, sl.placement.start_period, l.duration, ri });
    placement_of_[l.id] = sl.placement;
  }
}

void Checker::CheckMissingAndDuplicates() {
  for (size_t li = 0; li < m_.lessons.size(); ++li) {
    auto it = seen_count_.find(static_cast<int>(li));
    int n = it == seen_count_.end() ? 0 : it->second;
    if (n == 0) {
      Add(ConflictKind::kMissingLesson, "lesson is not scheduled",
        { m_.lessons[li].id });
    }
    else if (n > 1) {
      Add(ConflictKind::kDuplicateLesson,
        "lesson is scheduled " + std::to_string(n) + " times",
        { m_.lessons[li].id });
    }
  }
}

}  // namespace validating
}  // namespace arrango
