// src/demo/demo_data.cc

#include "demo/demo_internal.h"

namespace arrango {
  namespace demo_detail {

    void AddDivisionLessons(Builder& b, Id division_id,
      const std::vector<Id>& division_groups,
      const std::vector<SubjectPlan>& plans, int division_ordinal) {
      for (const SubjectPlan& plan : plans) {
        const int pool_size =
          plan.teacher_pool ? static_cast<int>(plan.teacher_pool->size()) : 0;
        auto pool_teacher = [&] (int offset) -> Id {
          if (!plan.requires_teacher || pool_size == 0) return kNoId;
          return (*plan.teacher_pool)[(division_ordinal + offset +
            static_cast<int>(b.seed)) %
            pool_size];
          };
        auto apply_rooms = [&] (LessonInstance& lesson) {
          if (plan.home_rooms && lesson.teacher_id != kNoId) {
            lesson.allowed_room_designators = b.RoomsForTeacher(lesson.teacher_id);
          }
          else {
            lesson.allowed_room_designators = plan.room_designators;
          }
          };
        if (plan.split_ways == 0) {
          Id teacher = pool_teacher(0);
          for (int i = 0; i < plan.count; ++i) {
            LessonInstance lesson;
            lesson.id = b.NewId();
            lesson.participants = { {division_id, kNoId} };
            lesson.subject_id = plan.subject;
            lesson.teacher_id = teacher;
            lesson.duration = plan.duration;
            lesson.requires_teacher = plan.requires_teacher;
            lesson.requires_room = plan.requires_room;
            apply_rooms(lesson);
            b.m.lessons.push_back(std::move(lesson));
          }
          continue;
        }
        const std::vector<Id>& split_groups =
          plan.groups_override ? *plan.groups_override : division_groups;
        const int ways = std::min<int>(plan.split_ways,
          static_cast<int>(split_groups.size()));
        for (int i = 0; i < plan.count; ++i) {
          Id block = plan.parallel ? b.NewId() : kNoId;
          for (int g = 0; g < ways; ++g) {
            LessonInstance lesson;
            lesson.id = b.NewId();
            lesson.participants = { {division_id, split_groups[g]} };
            lesson.subject_id = plan.subject;
            lesson.teacher_id = pool_teacher(g);
            lesson.duration = plan.duration;
            lesson.parallel_block_id = block;
            lesson.requires_teacher = plan.requires_teacher;
            lesson.requires_room = plan.requires_room;
            apply_rooms(lesson);
            b.m.lessons.push_back(std::move(lesson));
          }
        }
      }
    }

    std::vector<Id> MakePool(Builder& b, const std::string& tag, int n) {
      std::vector<Id> pool;
      for (int i = 0; i < n; ++i) pool.push_back(b.AddTeacher(tag));
      return pool;
    }

    // Locks the first lesson of (class, subject) to the given slot and room.
    void LockFirstLesson(Builder& b, Id division_id, Id subject_id, int day_index,
      uint32_t start, Id room_id) {
      for (LessonInstance& lesson : b.m.lessons) {
        bool of_division = false;
        for (const Participant& p : lesson.participants) {
          of_division |= p.division_id == division_id;
        }
        if (!of_division || lesson.subject_id != subject_id) continue;
        lesson.locked = true;
        lesson.locked_placement =
          Placement{ b.m.days[day_index].id, start, room_id };
        if (room_id != kNoId) {
          int r = -1;
          for (size_t i = 0; i < b.m.rooms.size(); ++i) {
            if (b.m.rooms[i].id == room_id) r = static_cast<int>(i);
          }
          if (r >= 0) {
            lesson.allowed_room_designators = { b.m.rooms[r].designator };
          }
        }
        return;
      }
    }

  }  // namespace demo_detail

  SchoolModel GenerateDemoSchool(DemoPreset preset, uint64_t seed) {
    demo_detail::Builder b;
    b.seed = seed % 7;  // rotates names/teacher assignment, never structure
    switch (preset) {
    case DemoPreset::kProduction:
      return demo_detail::BuildProduction(b);
    case DemoPreset::kMega:
      return demo_detail::BuildMega(b);
    }
    return {};
  }

}  // namespace arrango
