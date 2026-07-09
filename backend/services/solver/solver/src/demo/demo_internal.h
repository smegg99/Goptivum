// src/demo/demo_internal.h

#pragma once

#include <algorithm>
#include <array>
#include <map>
#include <string>
#include <tuple>
#include <vector>

#include "demo/demo_data.h"

namespace arrango {
  namespace demo_detail {

    inline constexpr std::array<const char*, 5> kDayNames = {
        "Poniedziałek", "Wtorek", "Środa", "Czwartek", "Piątek" };

    inline constexpr std::array<const char*, 30> kSurnames = {
        "Kowalska",   "Nowak",     "Wiśniewski", "Wójcik",    "Kowalczyk",
        "Kamińska",   "Lewandowski", "Zielińska", "Szymański", "Woźniak",
        "Dąbrowska",  "Kozłowski", "Jankowska",  "Mazur",     "Wojciechowski",
        "Kwiatkowska", "Krawczyk", "Kaczmarek",  "Piotrowska", "Grabowski",
        "Zając",      "Pawłowska", "Michalski",  "Król",      "Wieczorek",
        "Jabłońska",  "Wróbel",    "Nowakowska", "Majewski",  "Olszewska" };

    struct Builder {
      SchoolModel m;
      Id next_id = 1;
      uint64_t seed = 0;
      int teacher_counter = 0;
      // Generic classroom designators; teachers get two "home rooms" from this
      // pool so the candidate space stays realistic instead of
      // every-teacher-every-room.
      std::vector<std::string> sala_pool;
      std::map<Id, std::vector<std::string>> teacher_rooms;

      Id NewId() { return next_id++; }

      const std::vector<std::string>& RoomsForTeacher(Id teacher) {
        auto it = teacher_rooms.find(teacher);
        if (it != teacher_rooms.end()) return it->second;
        size_t i = teacher_rooms.size() + static_cast<size_t>(seed);
        std::vector<std::string> rooms = { sala_pool[i % sala_pool.size()],
                                          sala_pool[(i + 1) % sala_pool.size()] };
        return teacher_rooms.emplace(teacher, std::move(rooms)).first->second;
      }

      void Calendar(int days, int periods) {
        for (int d = 0; d < days; ++d) {
          m.days.push_back({ NewId(), kDayNames[d], static_cast<uint32_t>(periods) });
        }
        for (int p = 0; p < periods; ++p) {
          m.periods.push_back({ NewId(), std::to_string(p + 1) });
        }
      }

      Id AddYear(int level, uint32_t priority) {
        Id id = NewId();
        m.years.push_back({ id, "Rok " + std::to_string(level),
                           static_cast<uint32_t>(level), priority });
        return id;
      }

      Id AddDivision(const std::string& name, Id year) {
        Id id = NewId();
        Division division;
        division.id = id;
        division.name = name;
        division.year_id = year;
        m.divisions.push_back(std::move(division));
        return id;
      }

      Id AddSplit(Id division_id, const std::string& name, SplitKind kind) {
        Id id = NewId();
        m.splits.push_back({ id, name, division_id, kind });
        return id;
      }

      Id AddGroup(Id division_id, const std::string& name, Id split_id) {
        Id id = NewId();
        Group group;
        group.id = id;
        group.name = name;
        group.division_id = division_id;
        group.split_id = split_id;
        m.groups.push_back(std::move(group));
        return id;
      }

      Id AddTeacher(const std::string& subject_tag) {
        Id id = NewId();
        const char* surname =
          kSurnames[(teacher_counter + static_cast<int>(seed)) % kSurnames.size()];
        m.teachers.push_back(
          { id, std::string(surname) + " (" + subject_tag + ")", "" });
        ++teacher_counter;
        return id;
      }

      Id AddSubject(const std::string& name, bool prefers_blocks = false) {
        Id id = NewId();
        m.subjects.push_back({ id, name, prefers_blocks, "" });
        return id;
      }

      // Demo rooms use the name as designator; capacities stay unknown.
      Id AddRoom(const std::string& name) {
        Id id = NewId();
        Room room;
        room.id = id;
        room.name = name;
        room.designator = name;
        m.rooms.push_back(std::move(room));
        return id;
      }

      void AddExternalBlock(BlockTarget target, Id target_id,
        const std::string& name, int day_index, uint32_t start,
        uint32_t duration) {
        m.external_blocks.push_back({ NewId(), name, target, target_id,
                                     m.days[day_index].id, start, duration });
      }
    };

    // One subject's schedule requirements for one class.
    struct SubjectPlan {
      Id subject{};
      int count{};             // lesson instances (per group when split)
      uint32_t duration{ 1 };
      int split_ways{ 0 };       // 0 = whole class; >0 uses that many groups
      bool parallel{ false };    // split lessons of the same round run together
      std::vector<std::string> room_designators;
      bool requires_room{ true };
      bool requires_teacher{ true };
      // Restrict the lesson to the teacher's two home rooms instead of the
      // designator pool above (generic classroom subjects in bigger presets).
      bool home_rooms{ false };
      const std::vector<Id>* teacher_pool{};
      // Split the lesson over these groups instead of the division's default
      // ones (e.g. a FIXED gender split for PE). Must outlive the builder call.
      const std::vector<Id>* groups_override{};
    };

    // Shared preset helpers + the builders (demo_data.cc, demo_production.cc).
    void AddDivisionLessons(Builder& b, Id division_id,
      const std::vector<Id>& division_groups,
      const std::vector<SubjectPlan>& plans,
      int division_ordinal);
    std::vector<Id> MakePool(Builder& b, const std::string& tag, int n);
    void LockFirstLesson(Builder& b, Id division_id, Id subject_id, int day_index,
      uint32_t start, Id room_id);
    SchoolModel BuildProduction(Builder& b);
    SchoolModel BuildMega(Builder& b);

  }  // namespace demo_detail
}  // namespace arrango
