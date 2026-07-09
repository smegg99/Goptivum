// tests/validator_test.cc

#include <gtest/gtest.h>

#include <algorithm>

#include "validate/validator.h"

namespace arrango {
  namespace {

    // Hand-built model precise enough to trigger every conflict kind in
    // isolation. 2 days x 6 periods.
    //
    // Lessons: L1 1A Mat whole-division T1; L2/L3 1A Ang g1/g2 T2/T3 parallel B1;
    // L4 1B Mat T1; L5 1A Inf whole-division dur-2 T2 locked (day0,p2,R3);
    // L6 1B Projekt, no teacher, no room.
    constexpr Id kDay0 = 1, kDay1 = 2;
    constexpr Id kDivision1A = 20, kDivision1B = 21;
    constexpr Id kG1 = 30, kG2 = 31;
    constexpr Id kT1 = 40, kT2 = 41, kT3 = 42;
    constexpr Id kMat = 50, kAng = 51, kInf = 52, kProj = 53;
    constexpr Id kSala = 60, kPrac = 61;
    constexpr Id kR1 = 70, kR2 = 71, kR3 = 72;
    constexpr Id kL1 = 101, kL2 = 102, kL3 = 103, kL4 = 104, kL5 = 105,
      kL6 = 106;

    SchoolModel BaseModel() {
      SchoolModel m;
      m.days = { {kDay0, "Pon", 6}, {kDay1, "Wt", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300} };
      m.divisions = { {kDivision1A, "1A", 10}, {kDivision1B, "1B", 10} };
      m.groups = { {kG1, "g1", kDivision1A}, {kG2, "g2", kDivision1A} };
      m.teachers = { {kT1, "T1"}, {kT2, "T2"}, {kT3, "T3"} };
      m.subjects = { {kMat, "Matematyka"}, {kAng, "Angielski"},
                    {kInf, "Informatyka"}, {kProj, "Projekt"} };
      m.rooms = { {kR1, "R1", "R1"}, {kR2, "R2", "R2"}, {kR3, "R3", "R3"} };

      LessonInstance l1{ .id = kL1, .participants = {{kDivision1A, kNoId}},
                        .subject_id = kMat, .teacher_id = kT1,
                        .allowed_room_designators = {"R1", "R2"} };
      LessonInstance l2{ .id = kL2, .participants = {{kDivision1A, kG1}},
                        .subject_id = kAng, .teacher_id = kT2,
                        .allowed_room_designators = {"R1", "R2"},
                        .parallel_block_id = 5 };
      LessonInstance l3{ .id = kL3, .participants = {{kDivision1A, kG2}},
                        .subject_id = kAng, .teacher_id = kT3,
                        .allowed_room_designators = {"R1", "R2"},
                        .parallel_block_id = 5 };
      LessonInstance l4{ .id = kL4, .participants = {{kDivision1B, kNoId}},
                        .subject_id = kMat, .teacher_id = kT1,
                        .allowed_room_designators = {"R1", "R2"} };
      LessonInstance l5{ .id = kL5, .participants = {{kDivision1A, kNoId}},
                        .subject_id = kInf, .teacher_id = kT2, .duration = 2,
                        .allowed_room_designators = {"R3"}, .locked = true,
                        .locked_placement = Placement{kDay0, 2, kR3} };
      LessonInstance l6{ .id = kL6, .participants = {{kDivision1B, kNoId}},
                        .subject_id = kProj, .requires_teacher = false,
                        .requires_room = false };
      m.lessons = { l1, l2, l3, l4, l5, l6 };

      m.external_blocks = {
          {90, "T3 inna szkoła", BlockTarget::kTeacher, kT3, kDay1, 0, 2},
          {91, "1B basen", BlockTarget::kDivision, kDivision1B, kDay1, 4, 2},
          {92, "g2 praktyki", BlockTarget::kGroup, kG2, kDay1, 2, 1},
          {93, "R2 remont", BlockTarget::kRoom, kR2, kDay1, 0, 2},
      };
      // These fixtures isolate other conflict kinds; a school-default rule with
      // min 0 keeps the daily-load check out of the way (it has its own tests).
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      return m;
    }

    ScheduleSnapshot GoodSnapshot() {
      return { {
          {kL1, {kDay0, 0, kR1}},
          {kL2, {kDay0, 1, kR1}},
          {kL3, {kDay0, 1, kR2}},
          {kL4, {kDay0, 3, kR2}},
          {kL5, {kDay0, 2, kR3}},
          {kL6, {kDay0, 4, kNoId}},
      } };
    }

    void SetPlacement(ScheduleSnapshot& s, Id lesson, Placement p) {
      for (auto& sl : s.lessons) {
        if (sl.lesson_id == lesson) sl.placement = p;
      }
    }

    bool HasKind(const ValidationReport& r, ConflictKind kind) {
      return std::any_of(r.conflicts.begin(), r.conflicts.end(),
        [kind] (const Conflict& c) { return c.kind == kind; });
    }

    TEST(Validator, CleanScheduleIsValid) {
      ValidationReport r = Validate(BaseModel(), GoodSnapshot());
      EXPECT_TRUE(r.valid) << r.conflicts.size() << " conflicts, first: "
        << (r.conflicts.empty() ? ""
          : r.conflicts[0].message);
    }

    TEST(Validator, TeacherConflict) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL4, { kDay0, 0, kR2 });  // T1 teaches L1 at day0 p0 too
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_FALSE(r.valid);
      EXPECT_TRUE(HasKind(r, ConflictKind::kTeacherConflict));
    }

    TEST(Validator, WholeDivisionOverlapIsStudentOverlap) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL1, { kDay0, 2, kR1 });  // overlaps whole-division L5
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kStudentOverlap));
    }

    TEST(Validator, GroupVsWholeClassIsStudentOverlap) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL3, { kDay0, 0, kR2 });  // g2 lesson vs whole-division L1
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kStudentOverlap));
    }

    TEST(Validator, ParallelGroupsAreNotAConflict) {
      // L2 (g1) and L3 (g2) run at the same time in the good snapshot and that
      // must be fine.
      ValidationReport r = Validate(BaseModel(), GoodSnapshot());
      EXPECT_FALSE(HasKind(r, ConflictKind::kStudentOverlap));
    }

    // Split-less groups are implicit private OPEN splits: any of them may run
    // in parallel; only a group against itself or against a whole-division
    // lesson is a real student overlap.
    TEST(Validator, StudentOverlapOnlyWithinGroupOrWholeDivision) {
      SchoolModel m;
      m.days = { {1, "Pon", 4} };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300} };
      m.divisions = { {20, "1t", 10} };
      m.groups = { {30, "1/2", 20}, {31, "2/2", 20},
                  {32, "1/3", 20} };
      m.rooms = { {70, "R1", "R1"}, {71, "R2", "R2"} };
      m.lessons = {
          {.id = 100, .participants = {{20, 30}}, .subject_id = 0,
           .requires_teacher = false, .requires_room = false},  // 1/2
          {.id = 101, .participants = {{20, 32}}, .subject_id = 0,
           .requires_teacher = false, .requires_room = false},  // 1/3
          {.id = 102, .participants = {{20, 31}}, .subject_id = 0,
           .requires_teacher = false, .requires_room = false},  // 2/2
          {.id = 103, .participants = {{20, kNoId}}, .subject_id = 0,
           .requires_teacher = false, .requires_room = false},  // whole 1t
      };
      m.subjects = { {0, "x"} };

      // Three different groups at the same slot: distinct students, parallel OK.
      ScheduleSnapshot parallel{ {
          {100, {1, 0, kNoId}}, {101, {1, 0, kNoId}}, {102, {1, 0, kNoId}},
          {103, {1, 1, kNoId}},
      } };
      EXPECT_FALSE(HasKind(Validate(m, parallel), ConflictKind::kStudentOverlap));

      // Whole-division lesson at the same slot as one of its groups: real overlap.
      ScheduleSnapshot clash{ {
          {100, {1, 0, kNoId}}, {101, {1, 1, kNoId}}, {102, {1, 2, kNoId}},
          {103, {1, 0, kNoId}},
      } };
      EXPECT_TRUE(HasKind(Validate(m, clash), ConflictKind::kStudentOverlap));
    }

    // Fixed split vs open group at one slot: a real student overlap, and the
    // message must say which groups and why.
    TEST(Validator, FixedSplitOverlapReportedWithReason) {
      SchoolModel m = BaseModel();
      m.teachers.push_back({ 43, "T4", "" });
      m.splits = { {.id = 7, .name = "pe", .division_id = kDivision1A,
                   .kind = SplitKind::kFixed} };
      m.groups.push_back(
        { .id = 33, .name = "girls", .division_id = kDivision1A, .split_id = 7 });
      LessonInstance pe{ .id = 107, .participants = {{kDivision1A, 33}},
                        .subject_id = kMat, .teacher_id = 43,
                        .requires_room = false };
      m.lessons.push_back(pe);
      auto s = GoodSnapshot();
      s.lessons.push_back({ 107, {kDay0, 1, kNoId} });  // same slot as g1's L2

      ValidationReport r = Validate(m, s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kStudentOverlap));
      bool explained = false;
      for (const Conflict& c : r.conflicts) {
        if (c.kind != ConflictKind::kStudentOverlap) continue;
        explained |= c.message.find("girls") != std::string::npos &&
          c.message.find("pe") != std::string::npos;
      }
      EXPECT_TRUE(explained);
    }

    // Groups of two different OPEN splits at one slot: no student conflict.
    TEST(Validator, OpenSplitsParallelIsValid) {
      SchoolModel m = BaseModel();
      m.teachers.push_back({ 43, "T4", "" });
      m.splits = { {.id = 5, .name = "lang", .division_id = kDivision1A},
                  {.id = 6, .name = "inf", .division_id = kDivision1A} };
      m.groups[0].split_id = 5;  // g1 joins "lang"
      m.groups.push_back(
        { .id = 36, .name = "i1", .division_id = kDivision1A, .split_id = 6 });
      LessonInstance inf{ .id = 108, .participants = {{kDivision1A, 36}},
                         .subject_id = kInf, .teacher_id = 43,
                         .requires_room = false };
      m.lessons.push_back(inf);
      auto s = GoodSnapshot();
      s.lessons.push_back({ 108, {kDay0, 1, kNoId} });  // parallel with g1 and g2

      ValidationReport r = Validate(m, s);
      EXPECT_FALSE(HasKind(r, ConflictKind::kStudentOverlap));
    }

    TEST(Validator, UnknownOrForeignSplitReferenceReported) {
      SchoolModel m = BaseModel();
      m.groups[0].split_id = 999;  // no such split
      EXPECT_TRUE(HasKind(Validate(m, GoodSnapshot()),
        ConflictKind::kInvalidReference));

      SchoolModel m2 = BaseModel();
      m2.splits = { {.id = 5, .name = "x", .division_id = kDivision1B} };
      m2.groups[0].split_id = 5;  // split of another division
      EXPECT_TRUE(HasKind(Validate(m2, GoodSnapshot()),
        ConflictKind::kInvalidReference));
    }

    TEST(Validator, RoomConflict) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL4, { kDay0, 1, kR1 });  // R1 hosts L2 at day0 p1
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kRoomConflict));
    }

    TEST(Validator, MissingLesson) {
      auto s = GoodSnapshot();
      s.lessons.pop_back();  // drop L6
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kMissingLesson));
    }

    TEST(Validator, DuplicateLesson) {
      auto s = GoodSnapshot();
      s.lessons.push_back({ kL6, {kDay1, 2, kNoId} });
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kDuplicateLesson));
    }

    TEST(Validator, InvalidRoomMissingWhenRequired) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL1, { kDay0, 0, kNoId });
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kInvalidRoom));
    }

    TEST(Validator, InvalidRoomAssignedWhenNotTaken) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL6, { kDay0, 4, kR1 });
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kInvalidRoom));
    }

    TEST(Validator, DisallowedDesignatorIsInvalidRoom) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL1, { kDay0, 0, kR3 });  // R3 not in allowed {R1, R2}
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kInvalidRoom));
    }

    TEST(Validator, RoomTooSmallForKnownCounts) {
      SchoolModel m = BaseModel();
      // 1A whole-division lesson L1 placed in R1; give it 30 students and R1
      // a known capacity of 20.
      m.divisions[0].student_count = 30;
      m.divisions[0].count_source = CountSource::kImported;
      m.rooms[0].capacity = 20;  // R1
      m.rooms[0].capacity_source = CountSource::kImported;
      ValidationReport r = Validate(m, GoodSnapshot());  // L1 -> R1
      EXPECT_TRUE(HasKind(r, ConflictKind::kRoomTooSmall));
    }

    TEST(Validator, UnknownCapacityNeverTooSmall) {
      SchoolModel m = BaseModel();
      m.divisions[0].student_count = 30;
      m.divisions[0].count_source = CountSource::kImported;
      // R1 capacity stays unknown -> no kRoomTooSmall.
      ValidationReport r = Validate(m, GoodSnapshot());
      EXPECT_FALSE(HasKind(r, ConflictKind::kRoomTooSmall));
    }

    TEST(Validator, FixedRoomMismatchIsInvalidRoom) {
      SchoolModel m = BaseModel();
      m.lessons[0].fixed_room_id = kR2;  // L1 fixed to R2 but placed in R1
      ValidationReport r = Validate(m, GoodSnapshot());
      EXPECT_TRUE(HasKind(r, ConflictKind::kInvalidRoom));
    }

    // Daily-load violations on an imported/hand-built snapshot. One division,
    // no groups (one atom), 3 five-period days, 9 single-period lessons -> the
    // atom has weekly 9 over 3 active days, floor(9/3)=3, default min stays 3.
    SchoolModel DailyModel() {
      SchoolModel m;
      m.days = { {1, "A", 5}, {2, "B", 5}, {3, "C", 5} };
      for (Id p = 1; p <= 5; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300} };
      m.divisions = { {20, "1A", 10} };
      m.subjects = { {50, "x"} };
      for (Id i = 0; i < 9; ++i) {
        m.lessons.push_back({ .id = static_cast<Id>(100 + i),
                             .participants = {{20, kNoId}},
                             .subject_id = 50,
                             .requires_teacher = false,
                             .requires_room = false });
      }
      return m;
    }

    TEST(Validator, DailyLoadBelowMinimumIsViolation) {
      SchoolModel m = DailyModel();
      // Spread 3/3/3 -> valid. Move one lesson so a day has 2 (< min 3).
      ScheduleSnapshot s;
      int placements[9][2] = { {1, 0}, {1, 1}, {1, 2},  // day A: 3
                              {2, 0}, {2, 1},          // day B: 2 (violation)
                              {3, 0}, {3, 1}, {3, 2}, {3, 3} };  // day C: 4
      for (int i = 0; i < 9; i++) {
        s.lessons.push_back({ static_cast<Id>(100 + i),
                             {static_cast<Id>(placements[i][0]),
                              static_cast<uint32_t>(placements[i][1]), kNoId} });
      }
      EXPECT_TRUE(HasKind(Validate(m, s), ConflictKind::kDailyLoadViolation));
    }

    TEST(Validator, DailyLoadEmptyActiveDayIsViolation) {
      SchoolModel m = DailyModel();
      // All 9 on days A and C; day B empty (0 < min 3) -> violation.
      ScheduleSnapshot s;
      int placements[9][2] = { {1, 0}, {1, 1}, {1, 2}, {1, 3},
                              {3, 0}, {3, 1}, {3, 2}, {3, 3}, {3, 4} };
      for (int i = 0; i < 9; i++) {
        s.lessons.push_back({ static_cast<Id>(100 + i),
                             {static_cast<Id>(placements[i][0]),
                              static_cast<uint32_t>(placements[i][1]), kNoId} });
      }
      EXPECT_TRUE(HasKind(Validate(m, s), ConflictKind::kDailyLoadViolation));
    }

    TEST(Validator, DailyLoadAboveMaximumIsViolation) {
      SchoolModel m = DailyModel();
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0,
                             .max_per_day = 4} };
      ScheduleSnapshot s;
      int placements[9][2] = { {1, 0}, {1, 1}, {1, 2}, {1, 3}, {1, 4},  // 5 > max 4
                              {2, 0}, {2, 1}, {3, 0}, {3, 1} };
      for (int i = 0; i < 9; i++) {
        s.lessons.push_back({ static_cast<Id>(100 + i),
                             {static_cast<Id>(placements[i][0]),
                              static_cast<uint32_t>(placements[i][1]), kNoId} });
      }
      EXPECT_TRUE(HasKind(Validate(m, s), ConflictKind::kDailyLoadViolation));
    }

    TEST(Validator, DailyLoadTooFewWeeklyLessonsNoViolation) {
      SchoolModel m = DailyModel();
      m.lessons.resize(2);  // only 2 weekly over 3 days -> min relaxes to 0
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}}, {101, {1, 1, kNoId}}} };
      EXPECT_FALSE(HasKind(Validate(m, s), ConflictKind::kDailyLoadViolation));
    }

    // Attendance parity was intentionally removed: real vocational timetables run
    // different groups on different days, so a division whose groups attend on
    // different days is NOT a daily-load violation. One division split two ways;
    // disable the minimum so only the (now-absent) parity rule could fire.
    SchoolModel ParityModel() {
      SchoolModel m = DailyModel();
      m.groups = { {20 + 100, "1/2", 20}, {21 + 100, "2/2", 20} };
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      return m;
    }

    TEST(Validator, PartialAttendanceIsAllowed) {
      SchoolModel m = ParityModel();
      // Group 1/2 attends day A, group 2/2 attends a different day. With parity
      // relaxed and the minimum disabled, that is no longer a violation.
      m.lessons = { {.id = 100, .participants = {{20, 120}}, .subject_id = 50,
                    .requires_teacher = false, .requires_room = false},   // 1/2
                   {.id = 101, .participants = {{20, 121}}, .subject_id = 50,
                    .requires_teacher = false, .requires_room = false} };  // 2/2
      ScheduleSnapshot s{ {
          {100, {1, 0, kNoId}},  // 1/2 attends day A
          {101, {2, 0, kNoId}},  // 2/2 attends a DIFFERENT day
      } };
      EXPECT_FALSE(HasKind(Validate(m, s), ConflictKind::kDailyLoadViolation));
    }

    TEST(Validator, DuplicateDesignatorIsReported) {
      SchoolModel m = BaseModel();
      m.rooms[1].designator = "R1";  // now two rooms named R1
      ValidationReport r = Validate(m, GoodSnapshot());
      EXPECT_TRUE(HasKind(r, ConflictKind::kDuplicateDesignator));
    }

    TEST(Validator, UnknownDesignatorIsInvalidReference) {
      SchoolModel m = BaseModel();
      m.lessons[0].disallowed_room_designators = { "missing-room" };
      ValidationReport r = Validate(m, GoodSnapshot());
      EXPECT_TRUE(HasKind(r, ConflictKind::kInvalidReference));
    }

    TEST(Validator, ParallelBlockBroken) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL3, { kDay0, 4, kR2 });  // L2 stays at p1
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kParallelBlockBroken));
    }

    TEST(Validator, LockedMoved) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL5, { kDay0, 4, kR3 });
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kLockedMoved));
    }

    TEST(Validator, ExternalBlockOverlapClass) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL4, { kDay1, 4, kR1 });  // 1B basen day1 p4-5
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kExternalBlockOverlap));
    }

    TEST(Validator, ExternalBlockOverlapGroupBlocksWholeClass) {
      auto s = GoodSnapshot();
      // g2 blocked day1 p2; a whole-division 1A lesson there must conflict.
      SetPlacement(s, kL1, { kDay1, 2, kR1 });
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kExternalBlockOverlap));
    }

    TEST(Validator, ExternalBlockOverlapTeacherAndRoom) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL3, { kDay1, 0, kR2 });  // T3 blocked AND R2 blocked
      ValidationReport r = Validate(BaseModel(), s);
      int overlaps = 0;
      for (const auto& c : r.conflicts) {
        if (c.kind == ConflictKind::kExternalBlockOverlap) ++overlaps;
      }
      EXPECT_EQ(overlaps, 2);
    }

    TEST(Validator, OutOfBounds) {
      auto s = GoodSnapshot();
      SetPlacement(s, kL4, { kDay0, 9, kR2 });
      ValidationReport r = Validate(BaseModel(), s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kOutOfBounds));
    }

    TEST(Validator, InvalidReferenceUnknownLessonAndSubject) {
      SchoolModel m = BaseModel();
      m.lessons[0].subject_id = 999;
      auto s = GoodSnapshot();
      s.lessons.push_back({ 777, {kDay0, 0, kNoId} });
      ValidationReport r = Validate(m, s);
      EXPECT_TRUE(HasKind(r, ConflictKind::kInvalidReference));
    }

    TEST(Validator, InvalidDuration) {
      SchoolModel m = BaseModel();
      m.lessons[3].duration = 0;
      ValidationReport r = Validate(m, GoodSnapshot());
      EXPECT_TRUE(HasKind(r, ConflictKind::kInvalidDuration));
    }

  }  // namespace
}  // namespace arrango
