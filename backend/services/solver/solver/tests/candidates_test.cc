// tests/candidates_test.cc

#include <gtest/gtest.h>

#include "model/eligibility.h"
#include "solve/candidates.h"

namespace arrango {
  namespace {

    // 2 days x 4 periods, one class, one teacher, two sala rooms + one
    // pracownia.
    constexpr Id kDay0 = 1, kDay1 = 2;
    constexpr Id kY = 10, kC = 20, kG1 = 30, kT = 40, kMat = 50;
    constexpr Id kSala = 60, kPrac = 61;
    constexpr Id kR1 = 70, kR2 = 71, kR3 = 72;

    SchoolModel BaseModel() {
      SchoolModel m;
      m.days = { {kDay0, "Pon", 4}, {kDay1, "Wt", 4} };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {kY, "Rok 1", 1, 300} };
      m.divisions = { {kC, "1A", kY} };
      m.groups = { {kG1, "g1", kC} };
      m.teachers = { {kT, "T"} };
      m.subjects = { {kMat, "Mat"} };
      m.rooms = { {kR1, "R1", "R1"}, {kR2, "R2", "R2"}, {kR3, "R3", "R3"} };
      return m;
    }

    LessonInstance Lesson(Id id, uint32_t duration = 1) {
      return { .id = id, .participants = {{kC, kNoId}}, .subject_id = kMat,
              .teacher_id = kT, .duration = duration,
              .allowed_room_designators = {"R1", "R2"} };
    }

    CandidateSet Build(const SchoolModel& m) {
      auto result = BuildCandidates(m, ModelIndex::Build(m));
      EXPECT_TRUE(std::holds_alternative<CandidateSet>(result))
        << std::get<std::string>(result);
      return std::get<CandidateSet>(result);
    }

    TEST(Candidates, DayBoundsAndRoomEnumeration) {
      SchoolModel m = BaseModel();
      m.lessons = { Lesson(100, /*duration=*/2) };
      CandidateSet set = Build(m);
      // Duration 2 in 4 periods: starts 0..2, 2 days, 2 sala rooms = 12.
      EXPECT_EQ(set.all.size(), 12u);
      for (const Candidate& c : set.all) {
        EXPECT_LE(c.start + 2, 4u);
        EXPECT_TRUE(c.room_idx == 0 || c.room_idx == 1);  // never the pracownia
      }
    }

    TEST(Candidates, NoRoomLessonGetsRoomlessCandidates) {
      SchoolModel m = BaseModel();
      LessonInstance l = Lesson(100);
      l.requires_room = false;
      l.allowed_room_designators.clear();
      m.lessons = { l };
      CandidateSet set = Build(m);
      EXPECT_EQ(set.all.size(), 8u);  // 2 days x 4 starts
      for (const Candidate& c : set.all) EXPECT_EQ(c.room_idx, -1);
    }

    TEST(Candidates, LockedLessonHasExactlyOneCandidate) {
      SchoolModel m = BaseModel();
      LessonInstance l = Lesson(100);
      l.locked = true;
      l.locked_placement = Placement{ kDay1, 2, kR2 };
      m.lessons = { l };
      CandidateSet set = Build(m);
      ASSERT_EQ(set.all.size(), 1u);
      EXPECT_EQ(set.all[0].day_idx, 1);
      EXPECT_EQ(set.all[0].start, 2u);
      EXPECT_EQ(set.all[0].room_idx, 1);
    }

    TEST(Candidates, TeacherBlockPrunesOverlappingSlots) {
      SchoolModel m = BaseModel();
      m.lessons = { Lesson(100) };
      m.external_blocks = { {90, "b", BlockTarget::kTeacher, kT, kDay0, 0, 2} };
      CandidateSet set = Build(m);
      // Day0 starts 0,1 removed: (2 starts x 2 rooms) + day1 (4 x 2) = 12.
      EXPECT_EQ(set.all.size(), 12u);
      for (const Candidate& c : set.all) {
        EXPECT_FALSE(c.day_idx == 0 && c.start < 2);
      }
    }

    TEST(Candidates, RoomBlockPrunesOnlyThatRoom) {
      SchoolModel m = BaseModel();
      m.lessons = { Lesson(100) };
      m.external_blocks = { {90, "b", BlockTarget::kRoom, kR1, kDay0, 0, 4} };
      CandidateSet set = Build(m);
      // R1 unusable on day0 (4 candidates gone): 16 - 4 = 12.
      EXPECT_EQ(set.all.size(), 12u);
      for (const Candidate& c : set.all) {
        EXPECT_FALSE(c.day_idx == 0 && c.room_idx == 0);
      }
    }

    TEST(Candidates, ClassBlockPrunesGroupLessons) {
      SchoolModel m = BaseModel();
      LessonInstance l = Lesson(100);
      l.participants = { {kC, kG1} };
      m.lessons = { l };
      m.external_blocks = { {90, "b", BlockTarget::kDivision, kC, kDay0, 0, 4} };
      CandidateSet set = Build(m);
      for (const Candidate& c : set.all) EXPECT_EQ(c.day_idx, 1);
    }

    TEST(Candidates, GroupBlockPrunesWholeClassLessons) {
      SchoolModel m = BaseModel();
      m.lessons = { Lesson(100) };  // whole-division
      m.external_blocks = { {90, "b", BlockTarget::kGroup, kG1, kDay1, 0, 4} };
      CandidateSet set = Build(m);
      for (const Candidate& c : set.all) EXPECT_EQ(c.day_idx, 0);
    }

    TEST(Candidates, ImpossibleLockReturnsErrorNamingLesson) {
      SchoolModel m = BaseModel();
      LessonInstance l = Lesson(100);
      l.locked = true;
      l.locked_placement = Placement{ kDay0, 0, kR3 };  // pracownia not eligible
      m.lessons = { l };
      auto result = BuildCandidates(m, ModelIndex::Build(m));
      ASSERT_TRUE(std::holds_alternative<std::string>(result));
      EXPECT_NE(std::get<std::string>(result).find("100"), std::string::npos);
    }

    TEST(Candidates, FullyBlockedLessonReturnsError) {
      SchoolModel m = BaseModel();
      m.lessons = { Lesson(100) };
      m.external_blocks = { {90, "b1", BlockTarget::kDivision, kC, kDay0, 0, 4},
                           {91, "b2", BlockTarget::kDivision, kC, kDay1, 0, 4} };
      auto result = BuildCandidates(m, ModelIndex::Build(m));
      ASSERT_TRUE(std::holds_alternative<std::string>(result));
      EXPECT_NE(std::get<std::string>(result).find("100"), std::string::npos);
    }

    // --- External blocks follow the split rulebook (model/structure.h) ---------

    // Adds to BaseModel: open split "lang" {kG1, g2}, another open split "inf"
    // {i1}, fixed split "pe" {girls, boys}.
    SchoolModel SplitBlockModel() {
      SchoolModel m = BaseModel();
      m.splits = { {.id = 5, .name = "lang", .division_id = kC},
                  {.id = 6, .name = "inf", .division_id = kC},
                  {.id = 7, .name = "pe", .division_id = kC,
                   .kind = SplitKind::kFixed} };
      m.groups[0].split_id = 5;  // kG1 joins "lang"
      m.groups.push_back({ .id = 35, .name = "g2", .division_id = kC, .split_id = 5 });
      m.groups.push_back({ .id = 36, .name = "i1", .division_id = kC, .split_id = 6 });
      m.groups.push_back({ .id = 37, .name = "girls", .division_id = kC, .split_id = 7 });
      m.groups.push_back({ .id = 38, .name = "boys", .division_id = kC, .split_id = 7 });
      return m;
    }

    TEST(Candidates, FixedGroupBlockPrunesOpenGroupLessons) {
      SchoolModel m = SplitBlockModel();
      LessonInstance l = Lesson(100);
      l.participants = { {kC, kG1} };  // open "lang" group
      m.lessons = { l };
      // Girls (fixed split) busy all of day 0: some girls are in kG1, so kG1
      // cannot have lessons then either.
      m.external_blocks = { {90, "b", BlockTarget::kGroup, 37, kDay0, 0, 4} };
      CandidateSet set = Build(m);
      ASSERT_FALSE(set.all.empty());
      for (const Candidate& c : set.all) EXPECT_EQ(c.day_idx, 1);
    }

    TEST(Candidates, FixedGroupBlockSparesSiblingGroup) {
      SchoolModel m = SplitBlockModel();
      LessonInstance l = Lesson(100);
      l.participants = { {kC, 38} };  // boys
      m.lessons = { l };
      m.external_blocks = { {90, "b", BlockTarget::kGroup, 37, kDay0, 0, 4} };
      CandidateSet set = Build(m);
      // Girls busy never blocks boys: both days stay available.
      bool day0 = false;
      for (const Candidate& c : set.all) day0 |= c.day_idx == 0;
      EXPECT_TRUE(day0);
    }

    TEST(Candidates, OpenGroupBlockSparesOtherOpenSplits) {
      SchoolModel m = SplitBlockModel();
      LessonInstance l = Lesson(100);
      l.participants = { {kC, 36} };  // "inf" open group
      m.lessons = { l };
      // kG1 (open "lang") blocked: open-vs-open never shares students.
      m.external_blocks = { {90, "b", BlockTarget::kGroup, kG1, kDay0, 0, 4} };
      CandidateSet set = Build(m);
      bool day0 = false;
      for (const Candidate& c : set.all) day0 |= c.day_idx == 0;
      EXPECT_TRUE(day0);
    }

    // --- Candidate-count estimation --------------------------------------------

    TEST(Candidates, EstimateMatchesExactCountWithoutBlocks) {
      SchoolModel m = BaseModel();
      m.lessons = { Lesson(100, /*duration=*/2), Lesson(101) };
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(EstimateCandidateCount(m, ix, UnknownCapacityPolicy::kAllow),
        Build(m).all.size());
    }

    TEST(Candidates, EstimateIsUpperBoundUnderBlocksAndCountsLockedOnce) {
      SchoolModel m = BaseModel();
      LessonInstance locked = Lesson(100);
      locked.locked = true;
      locked.locked_placement = Placement{ kDay0, 0, kR1 };
      m.lessons = { locked, Lesson(101) };
      m.external_blocks = { {90, "b", BlockTarget::kTeacher, kT, kDay1, 0, 2} };
      ModelIndex ix = ModelIndex::Build(m);
      uint64_t estimate =
        EstimateCandidateCount(m, ix, UnknownCapacityPolicy::kAllow);
      CandidateSet set = Build(m);
      EXPECT_GE(estimate, set.all.size());
      // Locked lesson contributes exactly 1; the other 8 starts x 2 rooms.
      EXPECT_EQ(estimate, 1u + 16u);
    }

    // --- Capacity-aware eligibility (ResolveEligibleRooms) -------------------

    // Two rooms R1 (cap 16) and R2 (cap 30), plus R3 (unknown). Division kC has
    // 28 students; group kG1 has 14. Helper builds this fixture.
    SchoolModel CapacityModel() {
      SchoolModel m = BaseModel();
      m.divisions[0].student_count = 28;
      m.divisions[0].count_source = CountSource::kImported;
      m.groups[0].student_count = 14;
      m.groups[0].count_source = CountSource::kImported;
      m.rooms[0].capacity = 16;  // R1
      m.rooms[0].capacity_source = CountSource::kImported;
      m.rooms[1].capacity = 30;  // R2
      m.rooms[1].capacity_source = CountSource::kImported;
      // R3 keeps unknown capacity.
      return m;
    }

    TEST(Eligibility, DisallowedOverridesAllowed) {
      SchoolModel m = CapacityModel();
      LessonInstance l = Lesson(100);
      l.allowed_room_designators = { "R2" };
      l.disallowed_room_designators = { "R2" };
      RoomEligibility e =
        ResolveEligibleRooms(m, ModelIndex::Build(m), l, UnknownCapacityPolicy::kAllow);
      EXPECT_TRUE(e.rooms.empty());
    }

    TEST(Eligibility, RoomTooSmallForWholeDivision) {
      SchoolModel m = CapacityModel();
      LessonInstance l = Lesson(100);  // whole division (28), R1 cap 16 excluded
      l.allowed_room_designators = { "R1", "R2" };
      RoomEligibility e = ResolveEligibleRooms(m, ModelIndex::Build(m), l,
        UnknownCapacityPolicy::kAllow);
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(e.rooms, (std::vector<int>{ix.RoomIdx(kR2)}));
    }

    TEST(Eligibility, SubgroupFitsSmallerRoom) {
      SchoolModel m = CapacityModel();
      LessonInstance l = Lesson(100);
      l.participants = { {kC, kG1} };  // 14 students -> R1 (cap 16) stays
      l.allowed_room_designators = { "R1" };
      RoomEligibility e = ResolveEligibleRooms(m, ModelIndex::Build(m), l,
        UnknownCapacityPolicy::kAllow);
      EXPECT_EQ(e.rooms.size(), 1u);
    }

    TEST(Eligibility, UnknownCapacityFollowsPolicy) {
      SchoolModel m = CapacityModel();
      LessonInstance l = Lesson(100);
      l.allowed_room_designators = { "R3" };  // unknown-capacity room only
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(ResolveEligibleRooms(m, ix, l, UnknownCapacityPolicy::kAllow)
        .rooms.size(),
        1u);
      EXPECT_EQ(ResolveEligibleRooms(m, ix, l, UnknownCapacityPolicy::kAllowPenalized)
        .rooms.size(),
        1u);
      EXPECT_TRUE(ResolveEligibleRooms(m, ix, l, UnknownCapacityPolicy::kForbid)
        .rooms.empty());
    }

    TEST(Eligibility, UnknownCountNeverCapacityFilters) {
      SchoolModel m = CapacityModel();
      m.divisions[0].count_source = CountSource::kUnknown;  // count now unknown
      LessonInstance l = Lesson(100);
      l.allowed_room_designators = { "R1", "R3" };
      ModelIndex ix = ModelIndex::Build(m);
      // Even kForbid keeps everything: with no known count there is nothing to
      // check capacity against.
      EXPECT_EQ(ResolveEligibleRooms(m, ix, l, UnknownCapacityPolicy::kForbid)
        .rooms.size(),
        2u);
    }

    // --- Exact-room locks (fixed_room_id) -------------------------------------

    TEST(Eligibility, FixedRoomRestrictsToThatRoom) {
      SchoolModel m = BaseModel();
      LessonInstance l = Lesson(100);  // allowed {R1, R2}
      l.fixed_room_id = kR2;
      ModelIndex ix = ModelIndex::Build(m);
      RoomEligibility e =
        ResolveEligibleRooms(m, ix, l, UnknownCapacityPolicy::kAllow);
      EXPECT_EQ(e.rooms, (std::vector<int>{ix.RoomIdx(kR2)}));
    }

    // HARD lateness prunes candidates at the source: slots crossing the late
    // threshold never exist, and subject scoping prunes only that subject.
    TEST(Candidates, HardLatePrunesLateSlots) {
      SchoolModel m;
      m.days = { {1, "Pon", 9} };
      for (Id p = 1; p <= 9; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      for (auto [id, subject] : { std::pair<Id, Id>{100, 50}, {101, 51} }) {
        m.lessons.push_back({ .id = id, .participants = {{20, kNoId}},
                             .subject_id = subject, .teacher_id = 40,
                             .requires_room = false });
      }
      // Default threshold = 7: periods 7 and 8 are "late".
      m.rule_config.overrides = {
          {"late_student", RuleMode::kHard, 0, 0, kNoId, kNoId, /*subject=*/50} };
      ModelIndex ix = ModelIndex::Build(m);
      RuleResolver rules = RuleResolver::Build(m, ix, {});
      auto result =
        BuildCandidates(m, ix, UnknownCapacityPolicy::kAllow, 0, &rules);
      ASSERT_TRUE(std::holds_alternative<CandidateSet>(result));
      const CandidateSet& cs = std::get<CandidateSet>(result);
      EXPECT_EQ(cs.by_lesson[0].size(), 7u);  // maths: starts 0..6 only
      EXPECT_EQ(cs.by_lesson[1].size(), 9u);  // english: unpruned
    }

    TEST(Candidates, FixedRoomLessonOnlyGetsThatRoom) {
      SchoolModel m = BaseModel();
      LessonInstance l = Lesson(100);
      l.fixed_room_id = kR1;
      m.lessons = { l };
      CandidateSet set = Build(m);
      ASSERT_FALSE(set.all.empty());
      ModelIndex ix = ModelIndex::Build(m);
      for (const Candidate& c : set.all) EXPECT_EQ(c.room_idx, ix.RoomIdx(kR1));
    }

    TEST(Candidates, FixedRoomOutsideDesignatorsReturnsError) {
      SchoolModel m = BaseModel();
      LessonInstance l = Lesson(100);   // allowed {R1, R2}
      l.fixed_room_id = kR3;            // pracownia: not designator-eligible
      m.lessons = { l };
      auto result = BuildCandidates(m, ModelIndex::Build(m));
      ASSERT_TRUE(std::holds_alternative<std::string>(result));
      EXPECT_NE(std::get<std::string>(result).find("100"), std::string::npos);
      EXPECT_NE(std::get<std::string>(result).find("fixed"), std::string::npos);
    }

    TEST(Candidates, FixedRoomTooSmallReturnsError) {
      SchoolModel m = CapacityModel();
      LessonInstance l = Lesson(100);  // whole division (28)
      l.fixed_room_id = kR1;           // cap 16: capacity-ineligible
      m.lessons = { l };
      auto result = BuildCandidates(m, ModelIndex::Build(m));
      ASSERT_TRUE(std::holds_alternative<std::string>(result));
      EXPECT_NE(std::get<std::string>(result).find("100"), std::string::npos);
    }

    TEST(Eligibility, CapacityRemovedAllFlag) {
      SchoolModel m = CapacityModel();
      LessonInstance l = Lesson(100);  // whole division (28)
      l.allowed_room_designators = { "R1" };  // only the too-small room
      RoomEligibility e = ResolveEligibleRooms(m, ModelIndex::Build(m), l,
        UnknownCapacityPolicy::kAllow);
      EXPECT_TRUE(e.rooms.empty());
      EXPECT_TRUE(e.capacity_removed_all);
    }

  }  // namespace
}  // namespace arrango
