// src/model/index.cc

#include "model/index.h"

#include <algorithm>
#include <map>

namespace arrango {

  ModelIndex ModelIndex::Build(const SchoolModel& m) {
    ModelIndex ix;
    ix.model_ = &m;

    auto fill = [] (auto& map, const auto& items) {
      for (size_t i = 0; i < items.size(); ++i) {
        map.emplace(items[i].id, static_cast<int>(i));
      }
      };
    fill(ix.day_, m.days);
    fill(ix.year_, m.years);
    fill(ix.class_, m.divisions);
    fill(ix.group_, m.groups);
    fill(ix.teacher_, m.teachers);
    fill(ix.subject_, m.subjects);
    fill(ix.room_, m.rooms);
    fill(ix.lesson_, m.lessons);

    fill(ix.split_, m.splits);

    ix.groups_of_division_.resize(m.divisions.size());
    for (size_t g = 0; g < m.groups.size(); ++g) {
      int c = ix.DivisionIdx(m.groups[g].division_id);
      if (c >= 0) ix.groups_of_division_[c].push_back(static_cast<int>(g));
    }

    ix.groups_of_split_.resize(m.splits.size());
    ix.splits_of_division_.resize(m.divisions.size());
    for (size_t s = 0; s < m.splits.size(); ++s) {
      int c = ix.DivisionIdx(m.splits[s].division_id);
      if (c >= 0) ix.splits_of_division_[c].push_back(static_cast<int>(s));
    }
    ix.effective_split_of_group_.assign(m.groups.size(), -1);
    for (size_t g = 0; g < m.groups.size(); ++g) {
      int s = ix.SplitIdx(m.groups[g].split_id);
      int c = ix.DivisionIdx(m.groups[g].division_id);
      // A split of another division must not silently change semantics; treat
      // it as no split (the validator reports the bad reference).
      if (s >= 0 && c >= 0 &&
        ix.DivisionIdx(m.splits[s].division_id) == c) {
        ix.effective_split_of_group_[g] = s;
        ix.groups_of_split_[s].push_back(static_cast<int>(g));
      }
    }

    ix.lessons_of_division_.resize(m.divisions.size());
    ix.lessons_of_teacher_.resize(m.teachers.size());
    std::map<Id, std::vector<int>> blocks;
    for (size_t l = 0; l < m.lessons.size(); ++l) {
      const LessonInstance& lesson = m.lessons[l];
      for (const Participant& p : lesson.participants) {
        int c = ix.DivisionIdx(p.division_id);
        // Two group participants of one division must not double-list.
        if (c >= 0 && (ix.lessons_of_division_[c].empty() ||
          ix.lessons_of_division_[c].back() !=
          static_cast<int>(l))) {
          ix.lessons_of_division_[c].push_back(static_cast<int>(l));
        }
      }
      if (lesson.requires_teacher) {
        int t = ix.TeacherIdx(lesson.teacher_id);
        if (t >= 0) ix.lessons_of_teacher_[t].push_back(static_cast<int>(l));
      }
      if (lesson.parallel_block_id != kNoId) {
        blocks[lesson.parallel_block_id].push_back(static_cast<int>(l));
      }
    }
    for (auto& [id, members] : blocks) {
      if (members.size() >= 2) ix.parallel_blocks_.push_back(std::move(members));
    }

    return ix;
  }

  std::vector<int> ModelIndex::EligibleRooms(const LessonInstance& lesson) const {
    auto listed = [] (const std::vector<std::string>& v, const std::string& d) {
      return std::find(v.begin(), v.end(), d) != v.end();
      };
    std::vector<int> rooms;
    for (size_t r = 0; r < model_->rooms.size(); ++r) {
      const std::string& d = model_->rooms[r].designator;
      if (!lesson.allowed_room_designators.empty() &&
        !listed(lesson.allowed_room_designators, d)) {
        continue;
      }
      if (listed(lesson.disallowed_room_designators, d)) continue;
      rooms.push_back(static_cast<int>(r));
    }
    return rooms;
  }

  uint32_t ModelIndex::YearPriority(int year_idx) const {
    if (year_idx < 0 || year_idx >= static_cast<int>(model_->years.size())) {
      return 100;
    }
    return model_->years[year_idx].priority;
  }

}  // namespace arrango
