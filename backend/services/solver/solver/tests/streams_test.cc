// tests/streams_test.cc

#include <gtest/gtest.h>

#include <algorithm>
#include <iterator>

#include "model/atoms.h"
#include "model/streams.h"
#include "score/penalty_defs.h"

namespace arrango {
  namespace {

    // True iff the two lessons share at least one atom (sorted intersection).
    bool LessonsOverlap(const AtomSet& atoms, int lesson_a, int lesson_b) {
      const std::vector<int>& a = atoms.of_lesson[lesson_a];
      const std::vector<int>& b = atoms.of_lesson[lesson_b];
      std::vector<int> shared;
      std::set_intersection(a.begin(), a.end(), b.begin(), b.end(),
        std::back_inserter(shared));
      return !shared.empty();
    }

    // True iff `lesson` occupies `stream`: its atom set contains the
    // stream's atom.
    bool LessonInStream(int lesson_idx, const StudentStream& stream,
      const AtomSet& atoms) {
      const std::vector<int>& of = atoms.of_lesson[lesson_idx];
      return std::binary_search(of.begin(), of.end(), stream.atom_idx);
    }

    SchoolModel TwoClassModel() {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A"}, {.id = 21, .name = "1B"} };
      m.groups = { {.id = 30, .name = "g1", .division_id = 20},
                  {.id = 31, .name = "g2", .division_id = 20} };
      // Lessons drive atom -> stream membership below.
      m.lessons = { {.id = 1, .participants = {{20, kNoId}}},       // whole 1A
                   {.id = 2, .participants = {{20, 31}}},          // 1A g2
                   {.id = 3, .participants = {{21, kNoId}}},       // whole 1B
                   {.id = 4, .participants = {{20, 30}, {21, kNoId}}} };  // merged
      return m;
    }

    // 1A has an open "lang" split {1/2, 2/2} and a fixed "pe" split
    // {girls, boys}: streams must be the product "open group x fixed tuple".
    SchoolModel SplitModel() {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A"} };
      m.splits = { {.id = 5, .name = "lang", .division_id = 20,
                   .kind = SplitKind::kOpen},
                  {.id = 7, .name = "pe", .division_id = 20,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20, .split_id = 5},
                  {.id = 31, .name = "2/2", .division_id = 20, .split_id = 5},
                  {.id = 33, .name = "girls", .division_id = 20, .split_id = 7},
                  {.id = 34, .name = "boys", .division_id = 20, .split_id = 7} };
      return m;
    }

    TEST(Atoms, OpenTimesFixedTupleConstruction) {
      SchoolModel m = SplitModel();
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet a = BuildAtoms(m, ix);
      ASSERT_EQ(a.atoms.size(), 4u);  // {1/2, 2/2} x {girls, boys}
      EXPECT_EQ(a.of_division[0].size(), 4u);
      // Each open group appears in one atom per gender; each gender in one atom
      // per open group.
      EXPECT_EQ(a.of_group[ix.GroupIdx(30)].size(), 2u);
      EXPECT_EQ(a.of_group[ix.GroupIdx(33)].size(), 2u);
      // A 1/2 lesson and a girls lesson share students (the 1/2-girls atom).
      m.lessons = { {.id = 100, .participants = {{20, 30}}},
                   {.id = 101, .participants = {{20, 33}}},
                   {.id = 102, .participants = {{20, 31}}} };
      ix = ModelIndex::Build(m);
      a = BuildAtoms(m, ix);
      EXPECT_TRUE(LessonsOverlap(a, 0, 1));   // 1/2 vs girls
      EXPECT_FALSE(LessonsOverlap(a, 0, 2));  // 1/2 vs 2/2: same split, disjoint
    }

    TEST(Atoms, FixedOnlyDivisionGetsTupleAtoms) {
      SchoolModel m = SplitModel();
      m.splits[0].kind = SplitKind::kFixed;  // both splits fixed -> 2x2 tuples
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet a = BuildAtoms(m, ix);
      EXPECT_EQ(a.atoms.size(), 4u);
      // girls covers the two tuples containing girls.
      EXPECT_EQ(a.of_group[ix.GroupIdx(33)].size(), 2u);
    }

    TEST(Atoms, NoSplitsMatchesLegacyOneAtomPerGroup) {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A"} };
      m.groups = { {.id = 30, .name = "g1", .division_id = 20},
                  {.id = 31, .name = "g2", .division_id = 20} };
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet a = BuildAtoms(m, ix);
      ASSERT_EQ(a.atoms.size(), 2u);  // one atom per (implicitly open) group
      ASSERT_EQ(a.of_group[0].size(), 1u);
      ASSERT_EQ(a.of_group[1].size(), 1u);
      EXPECT_NE(a.of_group[0][0], a.of_group[1][0]);
    }

    TEST(Streams, OnePerAtom) {
      SchoolModel m = TwoClassModel();
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      auto streams = BuildStudentStreams(m, ix, atoms);
      // 1A splits into 2 atoms; 1B has 1 -> 3 streams.
      ASSERT_EQ(streams.size(), 3u);
      EXPECT_EQ(streams[0].division_idx, 0);
      EXPECT_EQ(streams[1].division_idx, 0);
      EXPECT_EQ(streams[2].division_idx, 1);
    }

    TEST(Streams, LessonMembership) {
      SchoolModel m = TwoClassModel();
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      auto streams = BuildStudentStreams(m, ix, atoms);

      // whole 1A (lesson 0) covers both 1A atoms, not 1B.
      EXPECT_TRUE(LessonInStream(0, streams[0], atoms));
      EXPECT_TRUE(LessonInStream(0, streams[1], atoms));
      EXPECT_FALSE(LessonInStream(0, streams[2], atoms));

      // 1A g2 (lesson 1) covers exactly one 1A atom.
      int g2_covered = (LessonInStream(1, streams[0], atoms) ? 1 : 0) +
        (LessonInStream(1, streams[1], atoms) ? 1 : 0);
      EXPECT_EQ(g2_covered, 1);

      // whole 1B (lesson 2) covers the 1B stream.
      EXPECT_TRUE(LessonInStream(2, streams[2], atoms));

      // Cross-division merged lesson (lesson 3) covers a 1A atom AND 1B.
      EXPECT_TRUE(LessonInStream(3, streams[2], atoms));
    }

    // A division with five named groups. Each group is its own atom (five atoms);
    // a whole-division lesson covers all five.
    SchoolModel FiveGroupModel() {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1t"} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20},
                  {.id = 31, .name = "2/2", .division_id = 20},
                  {.id = 32, .name = "1/3", .division_id = 20},
                  {.id = 33, .name = "2/3", .division_id = 20},
                  {.id = 34, .name = "3/3", .division_id = 20} };
      return m;
    }

    TEST(Atoms, OneAtomPerGroup) {
      SchoolModel m = FiveGroupModel();
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      // Five groups -> five atoms; every group covers exactly its own atom.
      EXPECT_EQ(atoms.atoms.size(), 5u);
      EXPECT_EQ(atoms.of_division[0].size(), 5u);
      EXPECT_EQ(atoms.of_group[ix.GroupIdx(30)].size(), 1u);
      EXPECT_EQ(atoms.of_group[ix.GroupIdx(32)].size(), 1u);
    }

    TEST(Atoms, EveryGroupIsItsOwnAtom) {
      SchoolModel m = FiveGroupModel();
      m.groups.push_back({ .id = 35, .name = "wf", .division_id = 20 });  // no part
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      // A split-less group is still a distinct atom (implicit open split),
      // disjoint from the others -- it does not merge with the whole division.
      EXPECT_EQ(atoms.of_division[0].size(), 6u);
      EXPECT_EQ(atoms.of_group[ix.GroupIdx(35)].size(), 1u);
    }

    TEST(Atoms, NoGroupsMeansOneAtom) {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A"}, {.id = 21, .name = "1B"} };
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      EXPECT_EQ(atoms.atoms.size(), 2u);
      EXPECT_EQ(atoms.of_division[0].size(), 1u);
      EXPECT_EQ(atoms.of_division[1].size(), 1u);
    }

    TEST(Atoms, CrossDivisionLessonUnionsAtoms) {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "A"}, {.id = 21, .name = "B"} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 21},
                  {.id = 31, .name = "2/2", .division_id = 21} };
      // Lesson: all of A + half of B.
      m.lessons = { {.id = 100, .participants = {{20, kNoId}, {21, 30}}} };
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      // A: 1 atom, B: 2 atoms. Lesson covers A's atom + one of B's.
      EXPECT_EQ(atoms.of_lesson[0].size(), 2u);
    }

    TEST(Atoms, OverlapOnlyWithinAGroupOrWholeDivision) {
      SchoolModel m = FiveGroupModel();
      // Different groups never share students, so they may run in parallel; only a
      // group against itself or against a whole-division lesson overlaps.
      m.lessons = { {.id = 100, .participants = {{20, 30}}},       // 1/2
                   {.id = 101, .participants = {{20, 32}}},       // 1/3
                   {.id = 102, .participants = {{20, 31}}},       // 2/2
                   {.id = 103, .participants = {{20, 30}}},       // 1/2 again
                   {.id = 104, .participants = {{20, kNoId}}} };   // whole division
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      EXPECT_FALSE(LessonsOverlap(atoms, 0, 1));  // different groups: parallel OK
      EXPECT_FALSE(LessonsOverlap(atoms, 0, 2));  // different groups: parallel OK
      EXPECT_TRUE(LessonsOverlap(atoms, 0, 3));   // same group: overlap
      EXPECT_TRUE(LessonsOverlap(atoms, 0, 4));   // whole division vs its group
    }

    TEST(PenaltyDefs, StudentWeightScalesByPriority) {
      EXPECT_EQ(StudentWeight(120, 300), 360);
      EXPECT_EQ(StudentWeight(120, 100), 120);
      EXPECT_EQ(StudentWeight(40, 150), 60);
    }

    TEST(PenaltyDefs, LatePeriodCostGrowsPastThreshold) {
      EXPECT_EQ(LatePeriodCost(40, 6, 7), 0);
      EXPECT_EQ(LatePeriodCost(40, 7, 7), 40);
      EXPECT_EQ(LatePeriodCost(40, 9, 7), 120);
    }

    TEST(PenaltyDefs, WithDefaultsFillsZeroFields) {
      Weights w{};
      w.student_gap_base = 0;
      w.teacher_gap_base = 7;
      Weights filled = WithDefaults(w);
      EXPECT_EQ(filled.student_gap_base, Weights{}.student_gap_base);
      EXPECT_EQ(filled.teacher_gap_base, 7);
      EXPECT_EQ(filled.late_threshold_period, 7u);
      EXPECT_EQ(filled.gap_cap_per_day, 3u);
    }

  }  // namespace
}  // namespace arrango
