// src/validate/validator_occupancy.cc

#include <algorithm>
#include <optional>
#include <set>
#include <string>
#include <unordered_map>
#include <vector>

#include "model/structure.h"
#include "validate/validator_internal.h"

namespace arrango {
namespace validating {

// One bucket per (entity, day, period); reports slots with >= 2 lessons.
void Checker::CheckOccupancy() {
  struct Slot {
    std::vector<int> lessons;
  };
  auto key = [&] (int entity, const Placed& p, uint32_t period) {
    return (static_cast<int64_t>(entity) * m_.days.size() + p.day_idx) *
      m_.periods.size() +
      period;
    };
  std::unordered_map<int64_t, Slot> teacher_occ, room_occ;
  // Student overlap is checked pairwise per (division, slot) through
  // SharesStudents (model/structure.h) so the conflict can NAME the two
  // groups and the reason. Few lessons occupy one division-slot, so the
  // pairwise check stays cheap.
  struct Entry {
    int lesson_idx;
    Participant participant;
  };
  std::unordered_map<int64_t, std::vector<Entry>> division_occ;

  for (const Placed& p : placed_) {
    const LessonInstance& l = m_.lessons[p.lesson_idx];
    for (uint32_t q = p.start; q < p.start + p.duration; ++q) {
      if (l.requires_teacher) {
        int t = ix_.TeacherIdx(l.teacher_id);
        if (t >= 0) teacher_occ[key(t, p, q)].lessons.push_back(p.lesson_idx);
      }
      if (p.room_idx >= 0) {
        room_occ[key(p.room_idx, p, q)].lessons.push_back(p.lesson_idx);
      }
      for (const Participant& part : l.participants) {
        int c = ix_.DivisionIdx(part.division_id);
        if (c >= 0) division_occ[key(c, p, q)].push_back({p.lesson_idx, part});
      }
    }
  }

  auto report = [&] (const std::unordered_map<int64_t, Slot>& occ,
    ConflictKind kind, const std::string& what, auto entity_id_of,
    EntityKind entity_kind) {
      // A slot may collect the same lesson from several atoms; report each
      // distinct set of >= 2 lessons once (dedupe by sorted lesson list).
      std::set<std::vector<int>> reported;
      for (const auto& [k, slot] : occ) {
        std::vector<int> lessons = slot.lessons;
        std::sort(lessons.begin(), lessons.end());
        lessons.erase(std::unique(lessons.begin(), lessons.end()),
          lessons.end());
        if (lessons.size() < 2) continue;
        int entity = static_cast<int>(k / m_.periods.size() / m_.days.size());
        int day_idx = static_cast<int>(k / m_.periods.size() % m_.days.size());
        uint32_t period = static_cast<uint32_t>(k % m_.periods.size());
        auto sig = lessons;
        sig.push_back(day_idx);
        sig.push_back(static_cast<int>(period));
        if (!reported.insert(sig).second) continue;
        std::vector<Id> ids;
        for (int li : lessons) ids.push_back(m_.lessons[li].id);
        Add(kind, what + " double-booked", std::move(ids),
          entity_id_of(entity), m_.days[day_idx].id, period, entity_kind);
      }
    };
  report(teacher_occ, ConflictKind::kTeacherConflict, "teacher",
    [&] (int e) { return m_.teachers[e].id; }, EntityKind::kTeacher);
  report(room_occ, ConflictKind::kRoomConflict, "room",
    [&] (int e) { return m_.rooms[e].id; }, EntityKind::kRoom);

  // Human-readable participant name for conflict messages.
  auto describe = [&] (const Participant& p) -> std::string {
    if (p.group_id == kNoId) {
      int c = ix_.DivisionIdx(p.division_id);
      return "whole " + (c >= 0 ? m_.divisions[c].name : "?");
    }
    int g = ix_.GroupIdx(p.group_id);
    return g >= 0 ? m_.groups[g].name : "?";
    };
  // Which rulebook line fired (model/structure.h); assumes SharesStudents
  // already returned true for the pair.
  auto reason = [&] (const Participant& a, const Participant& b) -> std::string {
    if (a.group_id == kNoId || b.group_id == kNoId) {
      return "whole class overlaps every group";
    }
    if (a.group_id == b.group_id) return "same group";
    for (const Participant& p : { a, b }) {
      int g = ix_.GroupIdx(p.group_id);
      int s = g >= 0 ? ix_.EffectiveSplitOf(g) : -1;
      if (ix_.SplitIsFixed(s)) {
        return "fixed split '" + m_.splits[s].name +
          "' overlaps other splits";
      }
    }
    return "shared students";
    };

  std::set<std::vector<int>> pair_reported;  // {lesson_a, lesson_b, day, period}
  for (const auto& [k, entries] : division_occ) {
    const int division = static_cast<int>(k / m_.periods.size() / m_.days.size());
    const int day_idx = static_cast<int>(k / m_.periods.size() % m_.days.size());
    const uint32_t period = static_cast<uint32_t>(k % m_.periods.size());
    for (size_t i = 0; i < entries.size(); ++i) {
      for (size_t j = i + 1; j < entries.size(); ++j) {
        const Entry& a = entries[i];
        const Entry& b = entries[j];
        if (a.lesson_idx == b.lesson_idx) continue;
        if (!SharesStudents(m_, ix_, a.participant, b.participant)) continue;
        std::vector<int> sig{ std::min(a.lesson_idx, b.lesson_idx),
                             std::max(a.lesson_idx, b.lesson_idx),
                             day_idx, static_cast<int>(period) };
        if (!pair_reported.insert(std::move(sig)).second) continue;
        Add(ConflictKind::kStudentOverlap,
          "students double-booked: " + describe(a.participant) + " vs " +
          describe(b.participant) + " (" +
          reason(a.participant, b.participant) + ")",
          { m_.lessons[a.lesson_idx].id, m_.lessons[b.lesson_idx].id },
          m_.divisions[division].id, m_.days[day_idx].id, period,
          EntityKind::kDivision);
      }
    }
  }
}

void Checker::CheckParallelBlocks() {
  for (const auto& block : ix_.ParallelBlocks()) {
    std::optional<std::pair<int, uint32_t>> anchor;
    std::vector<Id> ids;
    bool broken = false;
    for (int li : block) {
      auto it = std::find_if(placed_.begin(), placed_.end(),
        [li] (const Placed& p) {
          return p.lesson_idx == li;
        });
      if (it == placed_.end()) continue;  // missing reported elsewhere
      ids.push_back(m_.lessons[li].id);
      std::pair<int, uint32_t> at{ it->day_idx, it->start };
      if (!anchor) {
        anchor = at;
      }
      else if (*anchor != at) {
        broken = true;
      }
    }
    if (broken) {
      Add(ConflictKind::kParallelBlockBroken,
        "parallel block members start at different times", ids,
        m_.lessons[block[0]].parallel_block_id);
    }
  }
}

// Relative-placement links: evaluated on the snapshot exactly as the CP
// encoding enforces them (adjacency between chain neighbors; unordered =
// either orientation).
void Checker::CheckLessonLinks() {
  for (const LessonLink& link : m_.lesson_links) {
    std::vector<const Placed*> members;
    std::vector<Id> ids;
    for (Id id : link.lesson_ids) {
      int li = ix_.LessonIdx(id);
      if (li < 0) continue;  // preflight names bad references
      auto it = std::find_if(placed_.begin(), placed_.end(),
        [li] (const Placed& p) { return p.lesson_idx == li; });
      if (it == placed_.end()) continue;  // missing reported elsewhere
      members.push_back(&*it);
      ids.push_back(id);
    }
    if (members.size() < 2) continue;
    if (link.kind == LessonLinkKind::kSameDay ||
      link.kind == LessonLinkKind::kConsecutive) {
      bool same = true;
      for (const Placed* p : members) {
        same = same && p->day_idx == members[0]->day_idx;
      }
      if (!same) {
        Add(link.kind == LessonLinkKind::kSameDay
              ? ConflictKind::kLinkSameDay
              : ConflictKind::kLinkConsecutive,
          "link members sit on different days", ids, link.id);
        continue;
      }
    }
    if (link.kind == LessonLinkKind::kDifferentDay) {
      std::set<int> days;
      bool shared = false;
      for (const Placed* p : members) {
        shared |= !days.insert(p->day_idx).second;
      }
      if (shared) {
        Add(ConflictKind::kLinkDifferentDay,
          "DIFFERENT_DAY link members share a day", ids, link.id);
      }
    }
    if (link.kind == LessonLinkKind::kConsecutive) {
      for (size_t i = 0; i + 1 < members.size(); ++i) {
        const Placed* a = members[i];
        const Placed* b = members[i + 1];
        const bool forward = b->start == a->start + a->duration;
        const bool backward = a->start == b->start + b->duration;
        if (link.ordered ? !forward : !(forward || backward)) {
          Add(ConflictKind::kLinkConsecutive,
            "CONSECUTIVE link members are not adjacent", ids, link.id);
          break;
        }
      }
    }
  }
}

// Edge lessons must be their STUDENTS' first/last lesson of the day: any
// same-stream lesson on the forbidden side breaks the placement.
void Checker::CheckEdgePlacement() {
  for (const Placed& p : placed_) {
    const LessonInstance& l = m_.lessons[p.lesson_idx];
    if (l.edge == EdgePlacement::kNone) continue;
    bool earlier = false, later = false;
    for (const Placed& q : placed_) {
      if (&q == &p || q.day_idx != p.day_idx) continue;
      bool shares = false;
      for (const Participant& a : l.participants) {
        for (const Participant& b : m_.lessons[q.lesson_idx].participants) {
          shares |= SharesStudents(m_, ix_, a, b);
        }
      }
      if (!shares) continue;
      if (q.start + q.duration <= p.start) earlier = true;
      if (q.start >= p.start + p.duration) later = true;
    }
    const bool first_ok = !earlier;
    const bool last_ok = !later;
    const bool ok = l.edge == EdgePlacement::kFirst ? first_ok
      : l.edge == EdgePlacement::kLast ? last_ok
      : (first_ok || last_ok);
    if (!ok) {
      Add(ConflictKind::kEdgePlacement,
        "edge lesson has same-stream lessons on the forbidden side",
        { l.id }, l.id);
    }
  }
}

void Checker::CheckExternalBlocks() {
  for (const ExternalBlock& blk : m_.external_blocks) {
    int day = ix_.DayIdx(blk.day_id);
    if (day < 0) {
      Add(ConflictKind::kInvalidReference,
        "external block references unknown day", {}, blk.id);
      continue;
    }
    for (const Placed& p : placed_) {
      if (p.day_idx != day) continue;
      if (p.start >= blk.start_period + blk.duration ||
        blk.start_period >= p.start + p.duration) {
        continue;  // no time overlap
      }
      const LessonInstance& l = m_.lessons[p.lesson_idx];
      bool hits = false;
      switch (blk.target) {
      case BlockTarget::kDivision:
        for (const Participant& part : l.participants) {
          hits |= part.division_id == blk.target_id;
        }
        break;
      case BlockTarget::kGroup: {
        // Blocks that group and whole-division lessons of its class.
        int g = ix_.GroupIdx(blk.target_id);
        if (g < 0) break;
        for (const Participant& part : l.participants) {
          hits |= part.group_id == blk.target_id ||
            (part.group_id == kNoId &&
              part.division_id == m_.groups[g].division_id);
        }
        break;
      }
      case BlockTarget::kTeacher:
        hits = l.requires_teacher && l.teacher_id == blk.target_id;
        break;
      case BlockTarget::kRoom:
        hits = p.room_idx >= 0 && m_.rooms[p.room_idx].id == blk.target_id;
        break;
      }
      if (hits) {
        Add(ConflictKind::kExternalBlockOverlap,
          "lesson overlaps external block '" + blk.name + "'", { l.id },
          blk.id, blk.day_id, p.start, EntityKind::kExternalBlock);
      }
    }
  }
}

}  // namespace validating
}  // namespace arrango
