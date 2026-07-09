// src/model/index.h

#pragma once

#include <unordered_map>
#include <vector>

#include "model/model.h"

namespace arrango {

  // Id -> vector-index lookups plus derived groupings. Tolerant of invalid
  // references: unknown ids resolve to -1 (the validator reports them; other
  // modules skip them).
  class ModelIndex {
  public:
    static ModelIndex Build(const SchoolModel& m);

    int DayIdx(Id id) const { return Lookup(day_, id); }
    int YearIdx(Id id) const { return Lookup(year_, id); }
    int DivisionIdx(Id id) const { return Lookup(class_, id); }
    int GroupIdx(Id id) const { return Lookup(group_, id); }
    int TeacherIdx(Id id) const { return Lookup(teacher_, id); }
    int SubjectIdx(Id id) const { return Lookup(subject_, id); }
    int RoomIdx(Id id) const { return Lookup(room_, id); }
    int LessonIdx(Id id) const { return Lookup(lesson_, id); }
    int SplitIdx(Id id) const { return Lookup(split_, id); }

    // Groups of a split / splits of a division, resolved once. A group whose
    // split_id is unset, unknown, or belongs to another division resolves to
    // the implicit private OPEN split (-1).
    const std::vector<int>& GroupsOfSplit(int split_idx) const {
      return groups_of_split_[split_idx];
    }
    const std::vector<int>& SplitsOfDivision(int division_idx) const {
      return splits_of_division_[division_idx];
    }
    int EffectiveSplitOf(int group_idx) const {
      return effective_split_of_group_[group_idx];
    }
    bool SplitIsFixed(int split_idx) const {
      return split_idx >= 0 &&
        model_->splits[split_idx].kind == SplitKind::kFixed;
    }

    const std::vector<int>& GroupsOfDivision(int division_idx) const {
      return groups_of_division_[division_idx];
    }
    // Lessons with any participant in the division (whole-class or group).
    const std::vector<int>& LessonsOfDivision(int division_idx) const {
      return lessons_of_division_[division_idx];
    }
    // Only lessons with requires_teacher and a known teacher.
    const std::vector<int>& LessonsOfTeacher(int teacher_idx) const {
      return lessons_of_teacher_[teacher_idx];
    }
    // Lesson-index lists per parallel block, only blocks with >= 2 members.
    const std::vector<std::vector<int>>& ParallelBlocks() const {
      return parallel_blocks_;
    }

    // Rooms passing designator filtering: empty allowed list = all rooms;
    // disallowed always wins. Sorted room indices. Capacity and availability
    // are NOT applied here (see model/eligibility.h for the full resolver).
    std::vector<int> EligibleRooms(const LessonInstance& lesson) const;

    // Year priority for a year index, or 100 when the index is -1 (unknown).
    uint32_t YearPriority(int year_idx) const;

  private:
    using IdMap = std::unordered_map<Id, int>;
    static int Lookup(const IdMap& map, Id id) {
      auto it = map.find(id);
      return it == map.end() ? -1 : it->second;
    }

    const SchoolModel* model_{};
    IdMap day_, year_, class_, group_, teacher_, subject_, room_, lesson_,
      split_;
    std::vector<std::vector<int>> groups_of_division_;
    std::vector<std::vector<int>> lessons_of_division_;
    std::vector<std::vector<int>> lessons_of_teacher_;
    std::vector<std::vector<int>> parallel_blocks_;
    std::vector<std::vector<int>> groups_of_split_;
    std::vector<std::vector<int>> splits_of_division_;
    std::vector<int> effective_split_of_group_;
  };

}  // namespace arrango
