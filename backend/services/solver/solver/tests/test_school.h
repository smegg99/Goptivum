// tests/test_school.h

#pragma once

#include "model/model.h"

namespace arrango {

  // The hand-built small fixture that replaced the deleted tiny/small demo
  // presets (2026-07-09): deterministic, fast to solve, and PRISTINE-capable
  // (a gap-free, split-clean layout exists), so tests can assert exact
  // outcomes. 2 divisions (one with a 2-way OPEN split running parallel
  // language groups), 4 teachers, 2+2 rooms, 7 stream-periods per division
  // per week over 5x6 slots.
  inline SchoolModel TestSchool() {
    SchoolModel m;
    m.name = "Testowa";
    for (Id d = 1; d <= 5; ++d) m.days.push_back({ d, "D" + std::to_string(d), 6 });
    for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 100 + p, std::to_string(p) });
    m.years = { {10, "Rok 1", 1, 300}, {11, "Rok 3", 3, 100} };
    m.divisions = { {20, "1A", 10}, {21, "3A", 11} };
    m.splits = { {25, "jezyki", 20, SplitKind::kOpen} };
    m.groups = { {30, "1/2", 20, 0, CountSource::kUnknown, "", 25},
                {31, "2/2", 20, 0, CountSource::kUnknown, "", 25} };
    m.teachers = { {40, "TA"}, {41, "TB"}, {42, "TC"}, {43, "TD"} };
    m.subjects = { {50, "matematyka"}, {51, "j.polski"}, {52, "j.ang"} };
    m.rooms = { {70, "R1", "R1"}, {71, "R2", "R2"},
               {72, "J1", "J1"}, {73, "J2", "J2"} };

    Id next = 100;
    auto add = [&] (Id division, Id group, Id subject, Id teacher,
      std::vector<std::string> rooms, Id block = kNoId) {
        LessonInstance l;
        l.id = next++;
        l.participants = { {division, group} };
        l.subject_id = subject;
        l.teacher_id = teacher;
        l.allowed_room_designators = std::move(rooms);
        l.parallel_block_id = block;
        m.lessons.push_back(std::move(l));
      };
    for (Id division : {Id{ 20 }, Id{ 21 }}) {
      for (int i = 0; i < 3; ++i) add(division, kNoId, 50, 40, { "R1", "R2" });
      for (int i = 0; i < 2; ++i) add(division, kNoId, 51, 41, { "R1", "R2" });
    }
    // 1A's languages: two groups in parallel, twice a week.
    for (int i = 0; i < 2; ++i) {
      const Id block = next++;
      add(20, 30, 52, 42, { "J1", "J2" }, block);
      add(20, 31, 52, 43, { "J1", "J2" }, block);
    }
    // 3A takes whole-class english.
    for (int i = 0; i < 2; ++i) add(21, kNoId, 52, 42, { "J1", "J2" });
    return m;
  }

}  // namespace arrango
