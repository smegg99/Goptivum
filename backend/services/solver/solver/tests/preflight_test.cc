// tests/preflight_test.cc

#include <gtest/gtest.h>

#include <algorithm>
#include <string>

#include "validate/preflight.h"

namespace arrango {
  namespace {

    // 2 days x 4 periods (8 week slots), one division, one teacher, one room.
    SchoolModel Base() {
      SchoolModel m;
      m.days = { {1, "Pon", 4}, {2, "Wt", 4} };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"} };
      m.rooms = { {70, "R1", "R1"} };
      return m;
    }

    LessonInstance Roomless(Id id, uint32_t duration = 1) {
      LessonInstance l;
      l.id = id;
      l.participants = { {20, kNoId} };
      l.subject_id = 50;
      l.teacher_id = 40;
      l.duration = duration;
      l.requires_room = false;
      return l;
    }

    PreflightReport Preflight(const SchoolModel& m,
      uint32_t stream_cap = 0) {
      ModelIndex ix = ModelIndex::Build(m);
      RuleResolver rules = RuleResolver::Build(m, ix, {});
      return RunPreflight(m, ix, stream_cap, rules);
    }

    bool HasKind(const std::vector<Conflict>& v, ConflictKind kind,
      const std::string& needle) {
      return std::any_of(v.begin(), v.end(), [&] (const Conflict& c) {
        return c.kind == kind && c.message.find(needle) != std::string::npos;
        });
    }

    TEST(Preflight, TeacherPigeonholeNamesTheTeacher) {
      SchoolModel m = Base();
      for (Id id = 100; id < 109; ++id) m.lessons.push_back(Roomless(id));
      // 9 periods demanded, 8 week slots.
      PreflightReport r = Preflight(m);
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kTeacherOverloaded, "TA"));

      // External blocks tighten the bound: 8 demanded, 1 slot blocked -> hard.
      SchoolModel m2 = Base();
      for (Id id = 100; id < 108; ++id) m2.lessons.push_back(Roomless(id));
      EXPECT_TRUE(Preflight(m2).hard.empty());
      m2.external_blocks = { {90, "rada", BlockTarget::kTeacher, 40, 1, 3, 1} };
      EXPECT_TRUE(
        HasKind(Preflight(m2).hard, ConflictKind::kTeacherOverloaded, "TA"));
    }

    TEST(Preflight, RoomPoolOverloadNamesThePool) {
      SchoolModel m = Base();
      m.teachers.push_back({ 41, "TB", "" });
      m.teachers.push_back({ 42, "TC", "" });
      // 9 room-periods into a single 1-room pool with 8 week slots. Teachers
      // rotate so the teacher pigeonhole stays quiet.
      for (Id id = 100; id < 109; ++id) {
        LessonInstance l = Roomless(id);
        l.teacher_id = 40 + id % 3;
        l.requires_room = true;
        l.allowed_room_designators = { "R1" };
        m.lessons.push_back(l);
      }
      PreflightReport r = Preflight(m);
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kRoomPoolOverloaded, "R1"));
      EXPECT_FALSE(HasKind(r.hard, ConflictKind::kTeacherOverloaded, "T"));
    }

    TEST(Preflight, LockedCollisionNamesBothLessonsAndReason) {
      SchoolModel m = Base();
      for (Id id : {Id{ 100 }, Id{ 101 }}) {
        LessonInstance l = Roomless(id);
        l.locked = true;
        l.locked_placement = Placement{ 1, 0, kNoId };  // same slot, same teacher
        m.lessons.push_back(l);
      }
      PreflightReport r = Preflight(m);
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kLockedCollision, "same teacher"));
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kLockedCollision, "100"));
    }

    TEST(Preflight, MidDayBlockIsAdvisoryOnly) {
      SchoolModel m = Base();
      m.lessons.push_back(Roomless(100));
      m.external_blocks = { {90, "basen", BlockTarget::kDivision, 20, 1, 1, 2} };
      PreflightReport r = Preflight(m);
      EXPECT_TRUE(r.hard.empty());
      EXPECT_TRUE(
        HasKind(r.advisory, ConflictKind::kAvailabilityHole, "basen"));

      // Edge-of-day blocks are fine.
      m.external_blocks = { {90, "basen", BlockTarget::kDivision, 20, 1, 0, 2} };
      EXPECT_TRUE(Preflight(m).advisory.empty());
    }

    TEST(Preflight, StreamExplosionStillRefused) {
      SchoolModel m = Base();
      Id next = 500;
      for (int s = 0; s < 3; ++s) {
        Id split = next++;
        m.splits.push_back({ split, "fx", 20, SplitKind::kFixed });
        for (int g = 0; g < 5; ++g) {
          m.groups.push_back({ .id = next++, .name = "g", .division_id = 20,
                              .split_id = split });
        }
      }
      PreflightReport r = Preflight(m);  // 125 streams > default cap 64
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kStreamExplosion, "1A"));
      EXPECT_TRUE(Preflight(m, /*stream_cap=*/200).hard.empty());
    }

    // teach_daily / few_days semantic contradictions are named per teacher.
    TEST(Preflight, TeacherDayRuleContradictions) {
      SchoolModel m = Base();          // 2 active days
      m.lessons.push_back(Roomless(100));  // teacher 40 has ONE lesson
      m.rule_config.overrides = {
          {"teach_daily", RuleMode::kHard, 0, 0, kNoId, kNoId, kNoId, 40},
          {"few_days", RuleMode::kHard, 0, /*param=*/0, kNoId, kNoId, kNoId,
           40} };
      PreflightReport r = Preflight(m);
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kInvalidRuleConfig,
        "must teach daily"));
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kInvalidRuleConfig,
        "param >= 1"));
    }

    // A mistyped rule philosophy must refuse loudly, never silently fall back
    // to defaults.
    TEST(Preflight, BadRuleConfigIsRefusedByName) {
      SchoolModel m = Base();
      m.lessons.push_back(Roomless(100));
      m.rule_config.profile = "very_strict";              // unknown profile
      m.rule_config.overrides = { {"no_such_rule", RuleMode::kOff} };
      PreflightReport r = Preflight(m);
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kInvalidRuleConfig,
        "very_strict"));
      EXPECT_TRUE(HasKind(r.hard, ConflictKind::kInvalidRuleConfig,
        "no_such_rule"));
    }

    TEST(Preflight, GroupSizesOverDivisionIsAdvisory) {
      SchoolModel m = Base();
      m.divisions[0].student_count = 20;
      m.divisions[0].count_source = CountSource::kImported;
      m.splits.push_back({ 5, "lang", 20, SplitKind::kOpen });
      m.groups.push_back({ .id = 30, .name = "1/2", .division_id = 20,
                          .student_count = 15,
                          .count_source = CountSource::kImported, .split_id = 5 });
      m.groups.push_back({ .id = 31, .name = "2/2", .division_id = 20,
                          .student_count = 15,
                          .count_source = CountSource::kImported, .split_id = 5 });
      PreflightReport r = Preflight(m);  // 30 > 20
      EXPECT_TRUE(HasKind(r.advisory, ConflictKind::kGroupSizeMismatch, "lang"));
      EXPECT_TRUE(r.hard.empty());
    }

  }  // namespace
}  // namespace arrango
