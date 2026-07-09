// tests/model_index_test.cc

#include <gtest/gtest.h>

#include "model/index.h"
#include "model/model.h"

namespace arrango {
  namespace {

    SchoolModel SmallModel() {
      SchoolModel m;
      m.years = { {.id = 10, .name = "Rok 1", .level = 1, .priority = 300} };
      m.divisions = { {.id = 20, .name = "1A", .year_id = 10},
                   {.id = 21, .name = "1B", .year_id = 10} };
      m.groups = { {.id = 30, .name = "1A g1", .division_id = 20},
                  {.id = 31, .name = "1A g2", .division_id = 20} };
      m.teachers = { {.id = 40, .name = "Kowalska"}, {.id = 41, .name = "Nowak"} };
      m.subjects = { {.id = 50, .name = "Matematyka"} };
      m.rooms = { {.id = 70, .name = "s101", .designator = "s101"},
                 {.id = 71, .name = "s102", .designator = "s102"},
                 {.id = 72, .name = "s103", .designator = "s103"} };
      m.lessons = {
          {.id = 100, .participants = {{20, kNoId}}, .subject_id = 50,
           .teacher_id = 40, .parallel_block_id = 7},
          {.id = 101, .participants = {{20, 30}},
           .subject_id = 50, .teacher_id = 41, .parallel_block_id = 7},
          {.id = 102, .participants = {{21, kNoId}}, .subject_id = 50,
           .teacher_id = kNoId, .requires_teacher = false},
      };
      return m;
    }

    TEST(ModelIndex, IdLookups) {
      SchoolModel m = SmallModel();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(ix.DivisionIdx(20), 0);
      EXPECT_EQ(ix.DivisionIdx(21), 1);
      EXPECT_EQ(ix.DivisionIdx(999), -1);
      EXPECT_EQ(ix.TeacherIdx(41), 1);
      EXPECT_EQ(ix.LessonIdx(102), 2);
      EXPECT_EQ(ix.RoomIdx(kNoId), -1);
    }

    TEST(ModelIndex, DerivedGroupings) {
      SchoolModel m = SmallModel();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(ix.GroupsOfDivision(0), (std::vector<int>{0, 1}));
      EXPECT_TRUE(ix.GroupsOfDivision(1).empty());
      EXPECT_EQ(ix.LessonsOfDivision(0), (std::vector<int>{0, 1}));
      EXPECT_EQ(ix.LessonsOfDivision(1), (std::vector<int>{2}));
      // Lesson 102 has requires_teacher=false: must not appear anywhere.
      EXPECT_EQ(ix.LessonsOfTeacher(0), (std::vector<int>{0}));
      EXPECT_EQ(ix.LessonsOfTeacher(1), (std::vector<int>{1}));
      ASSERT_EQ(ix.ParallelBlocks().size(), 1u);
      EXPECT_EQ(ix.ParallelBlocks()[0], (std::vector<int>{0, 1}));
    }

    TEST(ModelIndex, EligibleRoomsEmptyAllowedMeansUnrestricted) {
      SchoolModel m = SmallModel();
      ModelIndex ix = ModelIndex::Build(m);
      LessonInstance lesson;
      EXPECT_EQ(ix.EligibleRooms(lesson), (std::vector<int>{0, 1, 2}));
    }

    TEST(ModelIndex, EligibleRoomsFiltersByDesignator) {
      SchoolModel m = SmallModel();
      ModelIndex ix = ModelIndex::Build(m);
      LessonInstance lesson;
      lesson.allowed_room_designators = { "s101", "s103", "nope" };  // unknown inert
      EXPECT_EQ(ix.EligibleRooms(lesson), (std::vector<int>{0, 2}));
    }

    TEST(ModelIndex, DisallowedDesignatorAlwaysWins) {
      SchoolModel m = SmallModel();
      ModelIndex ix = ModelIndex::Build(m);
      LessonInstance lesson;
      lesson.allowed_room_designators = { "s101", "s102" };
      lesson.disallowed_room_designators = { "s101" };
      EXPECT_EQ(ix.EligibleRooms(lesson), (std::vector<int>{1}));
      lesson.allowed_room_designators.clear();  // unrestricted minus disallowed
      EXPECT_EQ(ix.EligibleRooms(lesson), (std::vector<int>{1, 2}));
    }

    TEST(ModelIndex, SplitLookupsAndEffectiveSplit) {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A", .year_id = 10} };
      m.splits = { {.id = 5, .name = "languages", .division_id = 20,
                   .kind = SplitKind::kOpen},
                  {.id = 6, .name = "pe", .division_id = 20,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20, .split_id = 5},
                  {.id = 31, .name = "2/2", .division_id = 20, .split_id = 5},
                  {.id = 32, .name = "girls", .division_id = 20, .split_id = 6},
                  {.id = 33, .name = "loose", .division_id = 20} };
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(ix.SplitIdx(5), 0);
      EXPECT_EQ(ix.SplitIdx(99), -1);
      EXPECT_EQ(ix.GroupsOfSplit(0), (std::vector<int>{0, 1}));
      EXPECT_EQ(ix.GroupsOfSplit(1), (std::vector<int>{2}));
      EXPECT_EQ(ix.SplitsOfDivision(0), (std::vector<int>{0, 1}));
      EXPECT_EQ(ix.EffectiveSplitOf(0), 0);
      EXPECT_EQ(ix.EffectiveSplitOf(3), -1);  // split-less: implicit private open
      EXPECT_FALSE(ix.SplitIsFixed(-1));      // implicit splits are open
      EXPECT_FALSE(ix.SplitIsFixed(0));
      EXPECT_TRUE(ix.SplitIsFixed(1));
    }

    TEST(ModelIndex, SplitOfAnotherDivisionIsIgnored) {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A", .year_id = 10},
                     {.id = 21, .name = "1B", .year_id = 10} };
      m.splits = { {.id = 5, .name = "lang", .division_id = 21} };
      m.groups = { {.id = 30, .name = "g", .division_id = 20, .split_id = 5} };
      ModelIndex ix = ModelIndex::Build(m);
      // A cross-division split reference must not silently change semantics.
      EXPECT_EQ(ix.EffectiveSplitOf(0), -1);
      EXPECT_TRUE(ix.GroupsOfSplit(0).empty());
    }

    TEST(ModelIndex, YearPriorityFallsBackTo100) {
      SchoolModel m = SmallModel();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(ix.YearPriority(0), 300u);
      EXPECT_EQ(ix.YearPriority(-1), 100u);
    }

  }  // namespace
}  // namespace arrango
