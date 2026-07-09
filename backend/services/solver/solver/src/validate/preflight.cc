// src/validate/preflight.cc

#include "validate/preflight.h"

#include <algorithm>
#include <map>
#include <set>
#include <string>
#include <vector>

#include "model/structure.h"

namespace arrango {
  namespace {

    // Free slots per entity: week slots minus its external blocks (clamped
    // to day bounds; overlapping blocks may double-count, which only makes
    // the check LENIENT — a preflight must never produce false alarms).
    int64_t WeekSlots(const SchoolModel& m) {
      int64_t slots = 0;
      for (const Day& d : m.days) slots += d.period_count;
      return slots;
    }

    int64_t BlockedSlots(const SchoolModel& m, const ModelIndex& ix,
      BlockTarget target, Id target_id) {
      int64_t blocked = 0;
      for (const ExternalBlock& b : m.external_blocks) {
        if (b.target != target || b.target_id != target_id) continue;
        int d = ix.DayIdx(b.day_id);
        if (d < 0) continue;
        const uint32_t end =
          std::min(b.start_period + b.duration, m.days[d].period_count);
        if (end > b.start_period) blocked += end - b.start_period;
      }
      return blocked;
    }

    Conflict Finding(ConflictKind kind, std::string message, Id entity,
      EntityKind entity_kind) {
      Conflict c;
      c.kind = kind;
      c.message = std::move(message);
      c.entity_id = entity;
      if (entity != kNoId) c.locus.entities.push_back({ entity_kind, entity });
      return c;
    }

    // 1. Pigeonhole: a teacher's weekly lesson periods cannot exceed the
    // slots their availability leaves open.
    void CheckTeacherLoad(const SchoolModel& m, const ModelIndex& ix,
      PreflightReport& report) {
      const int64_t week = WeekSlots(m);
      for (size_t t = 0; t < m.teachers.size(); ++t) {
        int64_t demand = 0;
        for (int li : ix.LessonsOfTeacher(static_cast<int>(t))) {
          demand += m.lessons[li].duration;
        }
        if (demand == 0) continue;
        const int64_t free_slots =
          week - BlockedSlots(m, ix, BlockTarget::kTeacher, m.teachers[t].id);
        if (demand > free_slots) {
          report.hard.push_back(Finding(
            ConflictKind::kTeacherOverloaded,
            "teacher " + m.teachers[t].name + " needs " +
            std::to_string(demand) + " periods but only " +
            std::to_string(free_slots) + " are available",
            m.teachers[t].id, EntityKind::kTeacher));
        }
      }
    }

    // 2. Room demand: lessons restricted to one designator pool cannot
    // demand more periods than the pool can host in a week.
    void CheckRoomPools(const SchoolModel& m, const ModelIndex& ix,
      PreflightReport& report) {
      const int64_t week = WeekSlots(m);
      // Pool key = the exact sorted allowed-designator set.
      std::map<std::vector<std::string>, int64_t> demand;
      for (const LessonInstance& l : m.lessons) {
        if (!l.requires_room || l.allowed_room_designators.empty()) continue;
        std::vector<std::string> key = l.allowed_room_designators;
        std::sort(key.begin(), key.end());
        demand[std::move(key)] += l.duration;
      }
      for (const auto& [pool, periods] : demand) {
        int64_t rooms = 0;
        for (const Room& r : m.rooms) {
          rooms += std::count(pool.begin(), pool.end(), r.designator) > 0;
        }
        if (periods > rooms * week) {
          std::string names;
          for (const std::string& d : pool) {
            if (!names.empty()) names += ",";
            names += d;
          }
          report.hard.push_back(Finding(
            ConflictKind::kRoomPoolOverloaded,
            "rooms {" + names + "} must host " + std::to_string(periods) +
            " periods but fit " + std::to_string(rooms * week),
            kNoId, EntityKind::kUnspecified));
        }
      }
    }

    // 3. Two locked lessons colliding can never be satisfied.
    void CheckLockedCollisions(const SchoolModel& m, const ModelIndex& ix,
      PreflightReport& report) {
      std::vector<size_t> locked;
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        if (m.lessons[li].locked && m.lessons[li].locked_placement) {
          locked.push_back(li);
        }
      }
      for (size_t i = 0; i < locked.size(); ++i) {
        for (size_t j = i + 1; j < locked.size(); ++j) {
          const LessonInstance& a = m.lessons[locked[i]];
          const LessonInstance& b = m.lessons[locked[j]];
          const Placement& pa = *a.locked_placement;
          const Placement& pb = *b.locked_placement;
          if (pa.day_id != pb.day_id) continue;
          const bool overlap =
            pa.start_period < pb.start_period + b.duration &&
            pb.start_period < pa.start_period + a.duration;
          if (!overlap) continue;
          std::string why;
          if (a.requires_teacher && a.teacher_id == b.teacher_id) {
            why = "same teacher";
          }
          else if (pa.room_id != kNoId && pa.room_id == pb.room_id) {
            why = "same room";
          }
          else {
            for (const Participant& x : a.participants) {
              for (const Participant& y : b.participants) {
                if (SharesStudents(m, ix, x, y)) why = "shared students";
              }
            }
          }
          if (why.empty()) continue;
          Conflict c = Finding(
            ConflictKind::kLockedCollision,
            "locked lessons " + std::to_string(a.id) + " and " +
            std::to_string(b.id) + " collide (" + why + ")",
            kNoId, EntityKind::kUnspecified);
          c.lesson_ids = { a.id, b.id };
          report.hard.push_back(std::move(c));
        }
      }
    }

    // 4. Advisory: a blocked slot with free slots on BOTH sides of it on
    // one day traps entities into gaps (Dobry Plan refuses such windows).
    void CheckAvailabilityHoles(const SchoolModel& m, const ModelIndex& ix,
      PreflightReport& report) {
      for (const ExternalBlock& blk : m.external_blocks) {
        if (blk.target != BlockTarget::kTeacher &&
          blk.target != BlockTarget::kDivision) {
          continue;
        }
        int d = ix.DayIdx(blk.day_id);
        if (d < 0) continue;
        const uint32_t periods = m.days[d].period_count;
        const bool interior = blk.start_period > 0 &&
          blk.start_period + blk.duration < periods;
        if (!interior) continue;
        report.advisory.push_back(Finding(
          ConflictKind::kAvailabilityHole,
          "block '" + blk.name + "' sits mid-day (" + m.days[d].name +
          "): free slots on both sides invite gaps",
          blk.id, EntityKind::kExternalBlock));
      }
    }

    // 5. Stream cap: fixed splits multiplying past the cap mean the model
    // is almost certainly mis-declared (open splits marked fixed).
    void CheckStreamCap(const SchoolModel& m, const ModelIndex& ix,
      uint32_t cap, PreflightReport& report) {
      const std::vector<int64_t> streams = StreamCountPerDivision(m, ix);
      for (size_t c = 0; c < streams.size(); ++c) {
        if (streams[c] > static_cast<int64_t>(cap)) {
          report.hard.push_back(Finding(
            ConflictKind::kStreamExplosion,
            "division " + m.divisions[c].name + " produces " +
            std::to_string(streams[c]) + " student streams (cap " +
            std::to_string(cap) +
            "): share splits between subjects or mark them open",
            m.divisions[c].id, EntityKind::kDivision));
        }
      }
    }

    // 6. Advisory: a split whose known group sizes sum past the division.
    void CheckGroupSizes(const SchoolModel& m, const ModelIndex& ix,
      PreflightReport& report) {
      for (size_t s = 0; s < m.splits.size(); ++s) {
        int c = ix.DivisionIdx(m.splits[s].division_id);
        if (c < 0 ||
          m.divisions[c].count_source == CountSource::kUnknown) {
          continue;
        }
        int64_t sum = 0;
        bool all_known = !ix.GroupsOfSplit(static_cast<int>(s)).empty();
        for (int g : ix.GroupsOfSplit(static_cast<int>(s))) {
          if (m.groups[g].count_source == CountSource::kUnknown) {
            all_known = false;
            break;
          }
          sum += m.groups[g].student_count;
        }
        if (all_known && sum > m.divisions[c].student_count) {
          report.advisory.push_back(Finding(
            ConflictKind::kGroupSizeMismatch,
            "split '" + m.splits[s].name + "' of " + m.divisions[c].name +
            " sums to " + std::to_string(sum) + " students, division has " +
            std::to_string(m.divisions[c].student_count),
            m.splits[s].id, EntityKind::kUnspecified));
        }
      }
    }

    // 7. Semantic rule checks the generic resolver cannot see: per-teacher
    // teach_daily/few_days contradictions and pigeonholes.
    void CheckTeacherRuleConfig(const SchoolModel& m, const ModelIndex& ix,
      const RuleResolver& rules, PreflightReport& report) {
      uint32_t active_days = 0;
      for (const Day& d : m.days) active_days += d.period_count >= 1;
      for (size_t t = 0; t < m.teachers.size(); ++t) {
        const Id id = m.teachers[t].id;
        const ResolvedRule daily =
          rules.For("teach_daily", kNoId, kNoId, kNoId, id);
        const ResolvedRule few =
          rules.For("few_days", kNoId, kNoId, kNoId, id);
        const size_t lessons = ix.LessonsOfTeacher(static_cast<int>(t)).size();
        if (daily.mode == RuleMode::kHard && lessons > 0 &&
          lessons < active_days) {
          report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
            "teacher " + m.teachers[t].name + " must teach daily (hard) but "
            "has only " + std::to_string(lessons) + " lessons for " +
            std::to_string(active_days) + " days",
            id, EntityKind::kTeacher));
        }
        if (few.mode == RuleMode::kHard && few.param == 0) {
          report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
            "teacher " + m.teachers[t].name +
            ": few_days=hard needs param >= 1 (allowed active days)",
            id, EntityKind::kTeacher));
        }
        if (daily.mode == RuleMode::kHard && few.mode == RuleMode::kHard &&
          few.param > 0 && few.param < active_days && lessons > 0) {
          report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
            "teacher " + m.teachers[t].name + ": teach_daily=hard needs " +
            std::to_string(active_days) + " active days but few_days=hard "
            "allows only " + std::to_string(few.param),
            id, EntityKind::kTeacher));
        }
      }
    }

    // 8. Lesson-link sanity: bad references and lock contradictions are input
    // errors, named here instead of surfacing as bare infeasibility.
    void CheckLessonLinkConfig(const SchoolModel& m, const ModelIndex& ix,
      PreflightReport& report) {
      for (const LessonLink& link : m.lesson_links) {
        std::set<Id> seen;
        std::vector<const LessonInstance*> members;
        bool bad = false;
        for (Id id : link.lesson_ids) {
          int li = ix.LessonIdx(id);
          if (li < 0) {
            report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
              "lesson link " + std::to_string(link.id) +
              " names unknown lesson " + std::to_string(id),
              link.id, EntityKind::kUnspecified));
            bad = true;
            continue;
          }
          if (!seen.insert(id).second) {
            report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
              "lesson link " + std::to_string(link.id) +
              " lists lesson " + std::to_string(id) + " twice",
              link.id, EntityKind::kUnspecified));
            bad = true;
          }
          members.push_back(&m.lessons[li]);
        }
        if (!bad && members.size() < 2) {
          report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
            "lesson link " + std::to_string(link.id) + " needs 2+ lessons",
            link.id, EntityKind::kUnspecified));
          continue;
        }
        if (link.kind == LessonLinkKind::kConsecutive && !link.ordered &&
          members.size() > 2) {
          report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
            "lesson link " + std::to_string(link.id) +
            ": unordered CONSECUTIVE supports 2 lessons; set ordered for "
            "chains",
            link.id, EntityKind::kUnspecified));
        }
        // Locked members that already violate the link can never resolve.
        for (size_t i = 0; i < members.size(); ++i) {
          for (size_t j = i + 1; j < members.size(); ++j) {
            const LessonInstance* a = members[i];
            const LessonInstance* b = members[j];
            if (!a->locked || !a->locked_placement || !b->locked ||
              !b->locked_placement) {
              continue;
            }
            const bool same_day =
              a->locked_placement->day_id == b->locked_placement->day_id;
            if (link.kind == LessonLinkKind::kSameDay && !same_day) {
              report.hard.push_back(Finding(ConflictKind::kLinkSameDay,
                "SAME_DAY link " + std::to_string(link.id) +
                ": locked lessons sit on different days",
                link.id, EntityKind::kUnspecified));
            }
            if (link.kind == LessonLinkKind::kDifferentDay && same_day) {
              report.hard.push_back(Finding(ConflictKind::kLinkDifferentDay,
                "DIFFERENT_DAY link " + std::to_string(link.id) +
                ": locked lessons share a day",
                link.id, EntityKind::kUnspecified));
            }
          }
        }
      }
    }

  }  // namespace

  PreflightReport RunPreflight(const SchoolModel& m, const ModelIndex& ix,
    uint32_t max_streams_per_division, const RuleResolver& rules) {
    PreflightReport report;
    for (const std::string& mistake : rules.Diagnostics()) {
      report.hard.push_back(Finding(ConflictKind::kInvalidRuleConfig,
        mistake, kNoId, EntityKind::kUnspecified));
    }
    CheckTeacherRuleConfig(m, ix, rules, report);
    CheckLessonLinkConfig(m, ix, report);

    const uint32_t cap =
      max_streams_per_division > 0 ? max_streams_per_division : 64;
    CheckStreamCap(m, ix, cap, report);
    CheckTeacherLoad(m, ix, report);
    CheckRoomPools(m, ix, report);
    CheckLockedCollisions(m, ix, report);
    CheckAvailabilityHoles(m, ix, report);
    CheckGroupSizes(m, ix, report);
    return report;
  }

}  // namespace arrango
