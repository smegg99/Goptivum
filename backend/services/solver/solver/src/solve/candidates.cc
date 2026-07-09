// src/solve/candidates.cc

#include "solve/candidates.h"

#include <algorithm>
#include <utility>

#include "model/structure.h"
#include "score/penalty_defs.h"

namespace arrango {
  namespace {

    struct TimeBlock {
      int day_idx;
      uint32_t start;
      uint32_t duration;
    };

    bool Overlaps(const std::vector<TimeBlock>& blocks, int day_idx,
      uint32_t start, uint32_t duration) {
      for (const TimeBlock& b : blocks) {
        if (b.day_idx != day_idx) continue;
        if (start < b.start + b.duration && b.start < start + duration) {
          return true;
        }
      }
      return false;
    }

    // External blocks resolved to per-entity lists once, so the per-lesson
    // loop never rescans m.external_blocks.
    struct BlockedTime {
      std::vector<std::vector<TimeBlock>> division;
      std::vector<std::vector<TimeBlock>> group;
      std::vector<std::vector<TimeBlock>> teacher;
      std::vector<std::vector<TimeBlock>> room;
    };

    BlockedTime ResolveBlockedTime(const SchoolModel& m, const ModelIndex& ix) {
      BlockedTime blocked;
      blocked.division.resize(m.divisions.size());
      blocked.group.resize(m.groups.size());
      blocked.teacher.resize(m.teachers.size());
      blocked.room.resize(m.rooms.size());
      for (const ExternalBlock& blk : m.external_blocks) {
        int d = ix.DayIdx(blk.day_id);
        if (d < 0) continue;
        TimeBlock tb{ d, blk.start_period, blk.duration };
        switch (blk.target) {
        case BlockTarget::kDivision: {
          int c = ix.DivisionIdx(blk.target_id);
          if (c >= 0) blocked.division[c].push_back(tb);
          break;
        }
        case BlockTarget::kGroup: {
          int g = ix.GroupIdx(blk.target_id);
          if (g >= 0) blocked.group[g].push_back(tb);
          break;
        }
        case BlockTarget::kTeacher: {
          int t = ix.TeacherIdx(blk.target_id);
          if (t >= 0) blocked.teacher[t].push_back(tb);
          break;
        }
        case BlockTarget::kRoom: {
          int r = ix.RoomIdx(blk.target_id);
          if (r >= 0) blocked.room[r].push_back(tb);
          break;
        }
        }
      }
      return blocked;
    }

    // Input errors that make a lesson unschedulable regardless of the
    // calendar. Empty string = the lesson is well-formed.
    std::string LessonInputError(const ModelIndex& ix,
      const LessonInstance& l) {
      if (l.duration < 1) {
        return "lesson " + std::to_string(l.id) + " has zero duration";
      }
      if (l.requires_teacher && ix.TeacherIdx(l.teacher_id) < 0) {
        return "lesson " + std::to_string(l.id) +
          " requires a teacher but references none/unknown";
      }
      if (l.participants.empty()) {
        return "lesson " + std::to_string(l.id) + " has no participants";
      }
      return "";
    }

    // Room-dimension cap: keep only a rotated window of the eligible rooms
    // when there are far more than the lesson's time slots. Every kept room
    // is still a valid placement (no correctness impact); this only bounds
    // interchangeable-room choice so the model stays solvable.
    void CapRoomChoice(const SchoolModel& m, const ModelIndex& ix,
      const LessonInstance& l, size_t li, uint32_t max_candidates_per_lesson,
      std::vector<int>& rooms) {
      if (max_candidates_per_lesson == 0 || l.locked || rooms.size() <= 1) {
        return;
      }
      int slots = 0;
      for (const Day& day : m.days) {
        if (day.period_count >= l.duration) {
          slots += static_cast<int>(day.period_count - l.duration + 1);
        }
      }
      int room_cap = slots > 0
        ? std::max(1, static_cast<int>(max_candidates_per_lesson) / slots)
        : 1;
      if (static_cast<int>(rooms.size()) <= room_cap) return;
      // Keep a contiguous window of `room_cap` rooms, starting at a
      // per-lesson offset so lesson 0 keeps rooms 0..k, lesson 1 keeps
      // 1..k+1, ... — concurrent lessons spread over the whole room set
      // instead of all fighting for the same few.
      std::vector<int> kept;
      const size_t offset = li % rooms.size();
      for (int k = 0; k < room_cap; ++k) {
        kept.push_back(rooms[(offset + k) % rooms.size()]);
      }
      // Always keep the previous placement's room so re-solves warm-start.
      if (l.previous_placement && l.previous_placement->room_id != kNoId) {
        int pr = ix.RoomIdx(l.previous_placement->room_id);
        if (pr >= 0) kept.push_back(pr);
      }
      std::sort(kept.begin(), kept.end());
      kept.erase(std::unique(kept.begin(), kept.end()), kept.end());
      rooms = std::move(kept);
    }

    // Eligible room indices for one lesson ({-1} when it takes no room), or
    // an error string naming why no room fits.
    std::variant<std::vector<int>, std::string> RoomsForLesson(
      const SchoolModel& m, const ModelIndex& ix, const LessonInstance& l,
      size_t li, UnknownCapacityPolicy capacity_policy,
      uint32_t max_candidates_per_lesson) {
      if (!l.requires_room) return std::vector<int>{ -1 };
      RoomEligibility elig = ResolveEligibleRooms(m, ix, l, capacity_policy);
      std::vector<int> rooms = std::move(elig.rooms);
      if (rooms.empty()) {
        if (l.fixed_room_id != kNoId) {
          return "lesson " + std::to_string(l.id) +
            " is fixed to a room that is unknown, not allowed, or too small";
        }
        return "lesson " + std::to_string(l.id) +
          (elig.capacity_removed_all
            ? " has no room large enough for its participants"
            : " has no eligible room");
      }
      CapRoomChoice(m, ix, l, li, max_candidates_per_lesson, rooms);
      return rooms;
    }

    // Blocked-time lists that apply to THIS lesson, resolved once: the
    // teacher's blocks, each participant division's blocks, and every blocked
    // group that shares students with a participant per model/structure.h —
    // so a fixed-split block (girls busy) also freezes the division's open
    // groups, while sibling and open-vs-open groups stay free.
    std::vector<const std::vector<TimeBlock>*> BlockedForLesson(
      const SchoolModel& m, const ModelIndex& ix, const LessonInstance& l,
      int teacher_idx, const BlockedTime& blocked) {
      std::vector<const std::vector<TimeBlock>*> lists;
      if (teacher_idx >= 0 && !blocked.teacher[teacher_idx].empty()) {
        lists.push_back(&blocked.teacher[teacher_idx]);
      }
      for (const Participant& part : l.participants) {
        int c = ix.DivisionIdx(part.division_id);
        if (c >= 0 && !blocked.division[c].empty()) {
          lists.push_back(&blocked.division[c]);
        }
      }
      for (size_t g = 0; g < m.groups.size(); ++g) {
        if (blocked.group[g].empty()) continue;
        const Participant blocked_part{ m.groups[g].division_id,
                                        m.groups[g].id };
        for (const Participant& part : l.participants) {
          if (SharesStudents(m, ix, part, blocked_part)) {
            lists.push_back(&blocked.group[g]);
            break;
          }
        }
      }
      return lists;
    }

    // True when lateness is FORBIDDEN (rule mode HARD) for the teacher or any
    // participant division: slots crossing the late threshold never become
    // candidates at all.
    bool LateForbidden(const SchoolModel& m, const ModelIndex& ix,
      const LessonInstance& l, const RuleResolver* rules) {
      if (rules == nullptr) return false;
      if (l.requires_teacher &&
        rules->For("late_teacher", kNoId, kNoId, kNoId, l.teacher_id)
        .mode == RuleMode::kHard) {
        return true;
      }
      for (const Participant& part : l.participants) {
        int c = ix.DivisionIdx(part.division_id);
        if (c < 0) continue;
        if (rules->For("late_student", m.divisions[c].year_id,
          m.divisions[c].id, l.subject_id, kNoId)
          .mode == RuleMode::kHard) {
          return true;
        }
      }
      return false;
    }

    struct LockedSlot {
      int day_idx;
      int room_idx;
    };

    // Resolves a locked lesson's fixed placement against its eligible rooms
    // (sorted), or names the input error.
    std::variant<LockedSlot, std::string> ResolveLockedSlot(
      const ModelIndex& ix, const LessonInstance& l,
      const std::vector<int>& rooms) {
      if (!l.locked_placement) {
        return "lesson " + std::to_string(l.id) +
          " is locked without a locked placement";
      }
      const Placement& p = *l.locked_placement;
      int day_idx = ix.DayIdx(p.day_id);
      if (day_idx < 0) {
        return "lesson " + std::to_string(l.id) + " is locked to an unknown day";
      }
      int room_idx = -1;
      if (l.requires_room) {
        room_idx = ix.RoomIdx(p.room_id);
        if (room_idx < 0 ||
          !std::binary_search(rooms.begin(), rooms.end(), room_idx)) {
          return "lesson " + std::to_string(l.id) +
            " is locked to an ineligible room";
        }
      }
      else if (p.room_id != kNoId) {
        return "lesson " + std::to_string(l.id) +
          " takes no room but is locked to one";
      }
      return LockedSlot{ day_idx, room_idx };
    }

  }  // namespace

  std::variant<CandidateSet, std::string> BuildCandidates(
    const SchoolModel& m, const ModelIndex& ix,
    UnknownCapacityPolicy capacity_policy,
    uint32_t max_candidates_per_lesson, const RuleResolver* rules) {
    const uint32_t late_threshold =
      WithDefaults(m.weights).late_threshold_period;
    const BlockedTime blocked = ResolveBlockedTime(m, ix);

    CandidateSet set;
    set.by_lesson.resize(m.lessons.size());

    for (size_t li = 0; li < m.lessons.size(); ++li) {
      const LessonInstance& l = m.lessons[li];
      if (std::string err = LessonInputError(ix, l); !err.empty()) return err;
      const int t = l.requires_teacher ? ix.TeacherIdx(l.teacher_id) : -1;

      auto rooms_or_error = RoomsForLesson(m, ix, l, li, capacity_policy,
        max_candidates_per_lesson);
      if (std::holds_alternative<std::string>(rooms_or_error)) {
        return std::get<std::string>(rooms_or_error);
      }
      const std::vector<int>& rooms =
        std::get<std::vector<int>>(rooms_or_error);

      const std::vector<const std::vector<TimeBlock>*> blocked_for_lesson =
        BlockedForLesson(m, ix, l, t, blocked);
      auto time_blocked = [&] (int day_idx, uint32_t start) {
        for (const std::vector<TimeBlock>* blocks : blocked_for_lesson) {
          if (Overlaps(*blocks, day_idx, start, l.duration)) return true;
        }
        return false;
        };
      const bool no_late = LateForbidden(m, ix, l, rules);

      // The single point where a candidate is born: every pruning rule
      // (day bounds, hard lateness, blocked time, room blocks) is applied
      // here, for the locked and free paths alike.
      auto add_candidate = [&] (int day_idx, uint32_t start, int room_idx) {
        if (start + l.duration > m.days[day_idx].period_count) return;
        if (no_late && start + l.duration > late_threshold) return;
        if (time_blocked(day_idx, start)) return;
        if (room_idx >= 0 &&
          Overlaps(blocked.room[room_idx], day_idx, start, l.duration)) {
          return;
        }
        set.by_lesson[li].push_back(static_cast<int>(set.all.size()));
        set.all.push_back(
          { static_cast<int>(li), day_idx, start, room_idx });
        };

      if (l.locked) {
        auto slot_or_error = ResolveLockedSlot(ix, l, rooms);
        if (std::holds_alternative<std::string>(slot_or_error)) {
          return std::get<std::string>(slot_or_error);
        }
        const LockedSlot& slot = std::get<LockedSlot>(slot_or_error);
        add_candidate(slot.day_idx, l.locked_placement->start_period,
          slot.room_idx);
      }
      else {
        for (size_t d = 0; d < m.days.size(); ++d) {
          const uint32_t periods = m.days[d].period_count;
          if (periods < l.duration) continue;
          for (uint32_t start = 0; start + l.duration <= periods; ++start) {
            for (int room_idx : rooms) {
              add_candidate(static_cast<int>(d), start, room_idx);
            }
          }
        }
      }

      if (set.by_lesson[li].empty()) {
        return "lesson " + std::to_string(l.id) +
          " has no feasible placement (locked slot or external blocks "
          "eliminate all candidates)";
      }
    }
    return set;
  }

  uint64_t EstimateCandidateCount(const SchoolModel& m, const ModelIndex& ix,
    UnknownCapacityPolicy capacity_policy) {
    uint64_t total = 0;
    for (const LessonInstance& l : m.lessons) {
      if (l.locked) {
        ++total;  // a locked lesson builds exactly one candidate
        continue;
      }
      uint64_t rooms = 1;  // roomless lessons have one "no room" choice
      if (l.requires_room) {
        rooms = ResolveEligibleRooms(m, ix, l, capacity_policy).rooms.size();
      }
      // How many start periods fit the lesson across the week. A 2-period
      // lesson in a 4-period day can start at periods 0..2 -> 3 slots.
      uint64_t slots = 0;
      for (const Day& day : m.days) {
        if (day.period_count >= l.duration) {
          slots += day.period_count - l.duration + 1;
        }
      }
      total += slots * rooms;
    }
    return total;
  }

}  // namespace arrango
