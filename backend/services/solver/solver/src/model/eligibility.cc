// src/model/eligibility.cc

#include "model/eligibility.h"

namespace arrango {

  int ParticipantCount(const SchoolModel& m, const ModelIndex& ix,
    const LessonInstance& l) {
    int total = 0;
    for (const Participant& p : l.participants) {
      int c = ix.DivisionIdx(p.division_id);
      if (c < 0) continue;
      if (p.group_id == kNoId) {
        const Division& d = m.divisions[c];
        if (d.count_source == CountSource::kUnknown) return -1;
        total += static_cast<int>(d.student_count);
      }
      else {
        int g = ix.GroupIdx(p.group_id);
        if (g < 0) continue;
        const Group& grp = m.groups[g];
        if (grp.count_source == CountSource::kUnknown) return -1;
        total += static_cast<int>(grp.student_count);
      }
    }
    return total;
  }

  RoomEligibility ResolveEligibleRooms(const SchoolModel& m,
    const ModelIndex& ix,
    const LessonInstance& l,
    UnknownCapacityPolicy policy) {
    RoomEligibility result;
    const std::vector<int> by_designator = ix.EligibleRooms(l);
    const int count = ParticipantCount(m, ix, l);

    // An exact-room lock must still satisfy designators and capacity (the
    // validator rejects a fixed room that fails either), so it is applied as
    // a filter on the resolved set, not a bypass of it.
    const int fixed = l.fixed_room_id != kNoId ? ix.RoomIdx(l.fixed_room_id) : -1;

    for (int r : by_designator) {
      if (l.fixed_room_id != kNoId && r != fixed) continue;
      const Room& room = m.rooms[r];
      if (room.capacity_source != CountSource::kUnknown) {
        // Known capacity: drop only when it is smaller than a known count.
        if (count >= 0 && static_cast<int>(room.capacity) < count) continue;
      }
      else if (count >= 0 && policy == UnknownCapacityPolicy::kForbid) {
        // Unknown capacity, known demand, forbidding policy: drop.
        continue;
      }
      result.rooms.push_back(r);
    }

    result.capacity_removed_all =
      !by_designator.empty() && result.rooms.empty();
    return result;
  }

}  // namespace arrango
