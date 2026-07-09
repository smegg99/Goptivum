// src/validate/validator_rooms.cc

#include <algorithm>
#include <string>
#include <vector>

#include "model/eligibility.h"
#include "validate/validator_internal.h"

namespace arrango {
namespace validating {

void Checker::CheckRooms() {
  for (const Placed& p : placed_) {
    const LessonInstance& l = m_.lessons[p.lesson_idx];
    if (!l.requires_room) {
      if (p.room_idx >= 0) {
        Add(ConflictKind::kInvalidRoom,
          "lesson does not take a room but one is assigned", { l.id },
          m_.rooms[p.room_idx].id);
      }
      continue;
    }
    if (p.room_idx < 0) {
      Add(ConflictKind::kInvalidRoom,
        "lesson requires a room but none is assigned", { l.id });
      continue;
    }
    std::vector<int> eligible = ix_.EligibleRooms(l);
    if (!std::binary_search(eligible.begin(), eligible.end(), p.room_idx)) {
      Add(ConflictKind::kInvalidRoom,
        "room designator is not allowed for this lesson", { l.id },
        m_.rooms[p.room_idx].id, kNoId, 0, EntityKind::kRoom);
      continue;
    }
    // A lesson fixed to a specific room must be placed there.
    if (l.fixed_room_id != kNoId &&
      m_.rooms[p.room_idx].id != l.fixed_room_id) {
      Add(ConflictKind::kInvalidRoom, "lesson is fixed to a different room",
        { l.id }, m_.rooms[p.room_idx].id, kNoId, 0, EntityKind::kRoom);
      continue;
    }
    // Known capacity smaller than a known participant count.
    const Room& room = m_.rooms[p.room_idx];
    if (room.capacity_source != CountSource::kUnknown) {
      int count = ParticipantCount(m_, ix_, l);
      if (count >= 0 && static_cast<int>(room.capacity) < count) {
        Add(ConflictKind::kRoomTooSmall,
          "room capacity " + std::to_string(room.capacity) +
          " < participant count " + std::to_string(count),
          { l.id }, room.id, kNoId, 0, EntityKind::kRoom);
      }
    }
  }
}

void Checker::CheckLocked() {
  for (const Placed& p : placed_) {
    const LessonInstance& l = m_.lessons[p.lesson_idx];
    if (!l.locked || !l.locked_placement) continue;
    Placement actual{ m_.days[p.day_idx].id, p.start,
                     p.room_idx >= 0 ? m_.rooms[p.room_idx].id : kNoId };
    if (!(actual == *l.locked_placement)) {
      Add(ConflictKind::kLockedMoved, "locked lesson was moved", { l.id },
        kNoId, actual.day_id, actual.start_period);
    }
  }
}

}  // namespace validating
}  // namespace arrango
