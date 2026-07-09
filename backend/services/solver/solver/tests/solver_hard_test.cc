// tests/solver_hard_test.cc

#include <gtest/gtest.h>

#include <map>

#include "demo/demo_data.h"
#include "test_school.h"
#include "model/index.h"
#include "ortools/sat/cp_model.h"
#include "solve/candidates.h"
#include "solve/constraints.h"
#include "solve/solver.h"
#include "validate/validator.h"

namespace arrango {
  namespace {

    SolveParams TestParams(double seconds = 10.0) {
      return { .max_time_seconds = seconds, .num_workers = 1, .random_seed = 7 };
    }

    TEST(SolverHard, TestSchoolSolvesCleanly) {
      SchoolModel m = TestSchool();
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible)
        << r.message;
      EXPECT_EQ(r.best.lessons.size(), m.lessons.size());
      ValidationReport v = Validate(m, r.best);
      EXPECT_TRUE(v.valid) << (v.conflicts.empty() ? ""
        : v.conflicts[0].message);
    }

    // All four external-block target kinds on one model.
    TEST(SolverHard, SolvesCleanlyWithAllBlockKinds) {
      SchoolModel m = TestSchool();
      m.external_blocks = {
          {900, "rada", BlockTarget::kTeacher, 40, /*day=*/1, 0, 2},
          {901, "basen", BlockTarget::kDivision, 21, /*day=*/2, 0, 2},
          {902, "kolo", BlockTarget::kGroup, 30, /*day=*/3, 4, 2},
          {903, "remont", BlockTarget::kRoom, 73, /*day=*/4, 0, 6} };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible)
        << r.message;
      ValidationReport v = Validate(m, r.best);
      EXPECT_TRUE(v.valid) << (v.conflicts.empty() ? ""
        : v.conflicts[0].message);
    }

    // --- Split semantics, forced through a one-period day: the lessons MUST
    // share the slot, so the solve is feasible iff parallel is allowed. ---

    SchoolModel OneSlotModel() {
      SchoolModel m;
      m.days = { {1, "Pon", 1} };
      m.periods = { {201, ""} };
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"}, {41, "TB"}, {42, "TC"}, {43, "TD"} };
      m.subjects = { {50, "Mat"} };
      return m;
    }

    LessonInstance RoomlessLesson(Id id, Id group, Id teacher) {
      LessonInstance l;
      l.id = id;
      l.participants = { {20, group} };
      l.subject_id = 50;
      l.teacher_id = teacher;
      l.requires_room = false;
      return l;
    }

    TEST(SolverHard, OpenGroupsOfDifferentSplitsMayRunParallel) {
      SchoolModel m = OneSlotModel();
      m.splits = { {.id = 5, .name = "lang", .division_id = 20},
                  {.id = 6, .name = "inf", .division_id = 20} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20, .split_id = 5},
                  {.id = 32, .name = "i1", .division_id = 20, .split_id = 6} };
      m.lessons = { RoomlessLesson(100, 30, 40), RoomlessLesson(101, 32, 41) };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
    }

    TEST(SolverHard, FixedSplitForbidsParallelWithOpenGroup) {
      SchoolModel m = OneSlotModel();
      m.splits = { {.id = 5, .name = "lang", .division_id = 20},
                  {.id = 7, .name = "pe", .division_id = 20,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20, .split_id = 5},
                  {.id = 33, .name = "girls", .division_id = 20, .split_id = 7} };
      m.lessons = { RoomlessLesson(100, 30, 40), RoomlessLesson(101, 33, 41) };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kInfeasible) << r.message;
    }

    TEST(SolverHard, FixedSplitSiblingsRunParallel) {
      SchoolModel m = OneSlotModel();
      m.splits = { {.id = 7, .name = "pe", .division_id = 20,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 33, .name = "girls", .division_id = 20, .split_id = 7},
                  {.id = 34, .name = "boys", .division_id = 20, .split_id = 7} };
      m.lessons = { RoomlessLesson(100, 33, 40), RoomlessLesson(101, 34, 41) };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
    }

    TEST(SolverHard, TwoFixedSplitsNeverParallel) {
      SchoolModel m = OneSlotModel();
      m.splits = { {.id = 7, .name = "pe", .division_id = 20,
                   .kind = SplitKind::kFixed},
                  {.id = 8, .name = "rel", .division_id = 20,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 33, .name = "girls", .division_id = 20, .split_id = 7},
                  {.id = 35, .name = "rel", .division_id = 20, .split_id = 8} };
      m.lessons = { RoomlessLesson(100, 33, 40), RoomlessLesson(101, 35, 41) };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kInfeasible) << r.message;
    }

    TEST(SolverHard, WholeClassExcludesGroups) {
      SchoolModel m = OneSlotModel();
      m.splits = { {.id = 5, .name = "lang", .division_id = 20} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20, .split_id = 5} };
      m.lessons = { RoomlessLesson(100, kNoId, 40), RoomlessLesson(101, 30, 41) };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kInfeasible) << r.message;
    }

    TEST(SolverHard, FourWayOpenSplitParallel) {
      SchoolModel m = OneSlotModel();
      m.splits = { {.id = 9, .name = "workshop", .division_id = 20} };
      m.groups = { {.id = 60, .name = "w1", .division_id = 20, .split_id = 9},
                  {.id = 61, .name = "w2", .division_id = 20, .split_id = 9},
                  {.id = 62, .name = "w3", .division_id = 20, .split_id = 9},
                  {.id = 63, .name = "w4", .division_id = 20, .split_id = 9} };
      m.lessons = { RoomlessLesson(100, 60, 40), RoomlessLesson(101, 61, 41),
                   RoomlessLesson(102, 62, 42), RoomlessLesson(103, 63, 43) };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
    }

    // Three 5-group FIXED splits multiply into 125 student streams — almost
    // certainly a mis-declared model (open splits marked fixed). The preflight
    // must refuse it loudly instead of building a huge scoring model.
    TEST(SolverHard, StreamCapRefusesPathologicalFixedSplits) {
      SchoolModel m = OneSlotModel();
      Id next_id = 500;
      for (int s = 0; s < 3; ++s) {
        Id split = next_id++;
        m.splits.push_back({ split, "fx" + std::to_string(s), 20, SplitKind::kFixed });
        for (int g = 0; g < 5; ++g) {
          m.groups.push_back({ .id = next_id++, .name = "g", .division_id = 20,
                              .split_id = split });
        }
      }
      m.lessons = { RoomlessLesson(100, m.groups[0].id, 40) };

      SolveResult refused = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(refused.status, SolveStatusCode::kError);
      EXPECT_NE(refused.message.find("1A"), std::string::npos);
      EXPECT_NE(refused.message.find("streams"), std::string::npos);

      SolveParams params = TestParams();
      params.max_streams_per_division = 200;  // caller may raise the cap
      SolveResult solved = SolveSchedule(m, params, nullptr, nullptr);
      EXPECT_EQ(solved.status, SolveStatusCode::kOptimal) << solved.message;
    }

    // --- Rule modes at the CP encoding: hard rules constrain the search. ---

    // Three periods; english locked mid-day splits the two maths lessons into
    // two runs. Default (soft): feasible with a split penalty. subject_once
    // HARD: infeasible — no legal way to place maths in one run.
    TEST(SolverHard, HardSubjectOnceForbidsForcedDouble) {
      SchoolModel m;
      m.days = { {1, "Pon", 3} };
      for (Id p = 1; p <= 3; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      for (auto [id, subject] :
        { std::pair<Id, Id>{100, 50}, {101, 50}, {102, 51} }) {
        LessonInstance l;
        l.id = id;
        l.participants = { {20, kNoId} };
        l.subject_id = subject;
        l.teacher_id = 40;
        l.requires_room = false;
        m.lessons.push_back(l);
      }
      m.lessons[2].locked = true;  // english pinned to the middle period
      m.lessons[2].locked_placement = Placement{ 1, 1, kNoId };

      SolveResult soft = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(soft.status == SolveStatusCode::kOptimal ||
        soft.status == SolveStatusCode::kFeasible);

      m.rule_config.overrides = { {"subject_once", RuleMode::kHard} };
      SolveResult hard = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(hard.status, SolveStatusCode::kInfeasible);

      // podwójna dawka: the same hard school exempts maths -> feasible again.
      m.rule_config.overrides.push_back(
        { "subject_once", RuleMode::kOff, 0, 0, kNoId, kNoId, /*subject=*/50 });
      SolveResult dawka = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_TRUE(dawka.status == SolveStatusCode::kOptimal ||
        dawka.status == SolveStatusCode::kFeasible);
    }

    // Locked lessons at periods 0 and 2 force a single mid-day gap. Default:
    // feasible (soft near-hard). student_gaps HARD: infeasible.
    TEST(SolverHard, HardStudentGapsForbidForcedGap) {
      SchoolModel m;
      m.days = { {1, "Pon", 3} };
      for (Id p = 1; p <= 3; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      for (auto [id, subject, period] :
        { std::tuple<Id, Id, uint32_t>{100, 50, 0}, {101, 51, 2} }) {
        LessonInstance l;
        l.id = id;
        l.participants = { {20, kNoId} };
        l.subject_id = subject;
        l.teacher_id = 40;
        l.requires_room = false;
        l.locked = true;
        l.locked_placement = Placement{ 1, period, kNoId };
        m.lessons.push_back(l);
      }
      SolveResult soft = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(soft.status == SolveStatusCode::kOptimal ||
        soft.status == SolveStatusCode::kFeasible);

      m.rule_config.overrides = { {"student_gaps", RuleMode::kHard} };
      SolveResult hard = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(hard.status, SolveStatusCode::kInfeasible);
    }

    // --- Placement vocabulary: lesson links + edge-of-day. ---

    SchoolModel TwoDayModel() {
      SchoolModel m;
      m.days = { {1, "Pon", 4}, {2, "Wt", 4} };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10}, {21, "1B", 10} };
      m.teachers = { {40, "TA"}, {41, "TB"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      return m;
    }

    LessonInstance Plain(Id id, Id division, Id subject, Id teacher) {
      LessonInstance l;
      l.id = id;
      l.participants = { {division, kNoId} };
      l.subject_id = subject;
      l.teacher_id = teacher;
      l.requires_room = false;
      return l;
    }

    TEST(SolverHard, LessonLinksShapePlacements) {
      // SAME_DAY between disjoint divisions/teachers: nothing else couples
      // them, only the link can.
      SchoolModel m = TwoDayModel();
      m.lessons = { Plain(100, 20, 50, 40), Plain(101, 21, 51, 41) };
      m.lesson_links = { {900, LessonLinkKind::kSameDay, {100, 101}, false} };
      SolveResult same = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(same.status, SolveStatusCode::kOptimal) << same.message;
      EXPECT_EQ(same.best.lessons[0].placement.day_id,
        same.best.lessons[1].placement.day_id);

      // Locked apart + SAME_DAY: named by preflight, never bare infeasibility.
      m.lessons[0].locked = true;
      m.lessons[0].locked_placement = Placement{ 1, 0, kNoId };
      m.lessons[1].locked = true;
      m.lessons[1].locked_placement = Placement{ 2, 0, kNoId };
      SolveResult clash = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(clash.status, SolveStatusCode::kInfeasible);
      EXPECT_NE(clash.message.find("SAME_DAY"), std::string::npos);

      // DIFFERENT_DAY forces distinct days.
      SchoolModel diff = TwoDayModel();
      diff.lessons = { Plain(100, 20, 50, 40), Plain(101, 20, 50, 40) };
      diff.lesson_links = { {900, LessonLinkKind::kDifferentDay, {100, 101},
                            false} };
      SolveResult apart = SolveSchedule(diff, TestParams(), nullptr, nullptr);
      ASSERT_EQ(apart.status, SolveStatusCode::kOptimal) << apart.message;
      EXPECT_NE(apart.best.lessons[0].placement.day_id,
        apart.best.lessons[1].placement.day_id);

      // CONSECUTIVE (ordered): B directly after A, across divisions.
      SchoolModel consec = TwoDayModel();
      consec.lessons = { Plain(100, 20, 50, 40), Plain(101, 21, 51, 41) };
      consec.lesson_links = { {900, LessonLinkKind::kConsecutive, {100, 101},
        /*ordered=*/true} };
      SolveResult chain = SolveSchedule(consec, TestParams(), nullptr, nullptr);
      ASSERT_EQ(chain.status, SolveStatusCode::kOptimal) << chain.message;
      EXPECT_EQ(chain.best.lessons[0].placement.day_id,
        chain.best.lessons[1].placement.day_id);
      EXPECT_EQ(chain.best.lessons[1].placement.start_period,
        chain.best.lessons[0].placement.start_period + 1);

      // The validator flags a violating snapshot with the link kind.
      ValidationReport broken = Validate(
        consec, ScheduleSnapshot{ {{100, {1, 0, kNoId}}, {101, {1, 2, kNoId}}} });
      bool flagged = false;
      for (const Conflict& c : broken.conflicts) {
        flagged |= c.kind == ConflictKind::kLinkConsecutive;
      }
      EXPECT_TRUE(flagged);
    }

    TEST(SolverHard, EdgePlacementConstrainsStudentDays) {
      // 3 lessons, one day; the FIRST-edge lesson must open the day.
      SchoolModel m = TwoDayModel();
      m.days = { {1, "Pon", 4} };
      m.lessons = { Plain(100, 20, 50, 40), Plain(101, 20, 51, 40),
                   Plain(102, 20, 51, 40) };
      m.lessons[0].edge = EdgePlacement::kFirst;
      // Pin the companions so day 1 holds all three contiguously.
      m.lessons[1].locked = true;
      m.lessons[1].locked_placement = Placement{ 1, 1, kNoId };
      m.lessons[2].locked = true;
      m.lessons[2].locked_placement = Placement{ 1, 2, kNoId };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      for (const auto& sl : r.best.lessons) {
        if (sl.lesson_id == 100) EXPECT_EQ(sl.placement.start_period, 0u);
      }

      // FIRST + a companion locked at period 0 while the edge lesson can only
      // land later: infeasible.
      m.lessons[1].locked_placement = Placement{ 1, 0, kNoId };
      m.lessons[2].locked_placement = Placement{ 1, 1, kNoId };
      m.lessons[0].locked = true;
      m.lessons[0].locked_placement = Placement{ 1, 2, kNoId };
      EXPECT_EQ(SolveSchedule(m, TestParams(), nullptr, nullptr).status,
        SolveStatusCode::kInfeasible);
      // The validator names the same violation on the snapshot.
      ValidationReport v = Validate(
        m, ScheduleSnapshot{ {{100, {1, 2, kNoId}},
                             {101, {1, 0, kNoId}},
                             {102, {1, 1, kNoId}}} });
      bool flagged = false;
      for (const Conflict& c : v.conflicts) {
        flagged |= c.kind == ConflictKind::kEdgePlacement;
      }
      EXPECT_TRUE(flagged);
    }

    // Recipe e2e (distribution patterns as configs): "bloki lekcyjne" — a 3h
    // subject shaped 2+1 keeps its declared shapes via durations and lands the
    // instances on different days via a DIFFERENT_DAY link (subject_once alone
    // counts RUNS, so adjacent 2+1 would legally merge into a 3-block).
    TEST(SolverHard, BlockShapeRecipe) {
      SchoolModel m = TwoDayModel();
      m.lessons = { Plain(100, 20, 50, 40), Plain(101, 20, 50, 40) };
      m.lessons[0].duration = 2;
      m.lesson_links = { {900, LessonLinkKind::kDifferentDay, {100, 101}, false} };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      EXPECT_NE(r.best.lessons[0].placement.day_id,
        r.best.lessons[1].placement.day_id);
    }

    // REGRESSION (found via mega LNS zero-accept): edge placement must be
    // enforced by the HARD-constraint layer alone — the construct phase and
    // every LNS sub-model build hard constraints without the objective, and an
    // edge-violating construct incumbent poisons every LNS trial at the
    // validity gate. The raw hard model must refuse a forced violation.
    TEST(SolverHard, HardConstraintLayerAloneEnforcesEdgePlacement) {
      namespace sat = operations_research::sat;
      SchoolModel m = TwoDayModel();
      m.days = { {1, "Pon", 4} };
      m.lessons = { Plain(100, 20, 50, 40), Plain(101, 20, 51, 40) };
      m.lessons[0].edge = EdgePlacement::kFirst;
      m.lessons[0].locked = true;
      m.lessons[0].locked_placement = Placement{ 1, 2, kNoId };  // NOT first...
      m.lessons[1].locked = true;
      m.lessons[1].locked_placement = Placement{ 1, 1, kNoId };  // ...this is.

      ModelIndex ix = ModelIndex::Build(m);
      auto built = BuildCandidates(m, ix);
      ASSERT_TRUE(std::holds_alternative<CandidateSet>(built));
      const CandidateSet& cs = std::get<CandidateSet>(built);
      sat::CpModelBuilder cp;
      std::vector<sat::BoolVar> x;
      for (size_t i = 0; i < cs.all.size(); ++i) x.push_back(cp.NewBoolVar());
      AddHardConstraints(m, ix, cs, x, cp);  // construct-phase model: NO objective
      const sat::CpSolverResponse response = sat::Solve(cp.Build());
      EXPECT_EQ(response.status(), sat::CpSolverStatus::INFEASIBLE);
    }

    TEST(SolverHard, ConflictingLocksAreInfeasible) {
      SchoolModel m = TestSchool();
      // Lock two lessons of the same teacher to the same slot.
      std::map<Id, std::vector<size_t>> by_teacher;
      for (size_t i = 0; i < m.lessons.size(); ++i) {
        if (m.lessons[i].requires_teacher && !m.lessons[i].locked) {
          by_teacher[m.lessons[i].teacher_id].push_back(i);
        }
      }
      const std::vector<size_t>* pair = nullptr;
      for (const auto& [t, lessons] : by_teacher) {
        if (lessons.size() >= 2) {
          pair = &lessons;
          break;
        }
      }
      ASSERT_NE(pair, nullptr);
      ModelIndex ix = ModelIndex::Build(m);
      for (int k = 0; k < 2; ++k) {
        LessonInstance& l = m.lessons[(*pair)[k]];
        std::vector<int> rooms = ix.EligibleRooms(l);
        ASSERT_FALSE(rooms.empty());
        l.locked = true;
        // Different rooms, same time: only the teacher clashes.
        int room_idx = rooms[k % rooms.size()];
        l.locked_placement = Placement{ m.days[1].id, 1, m.rooms[room_idx].id };
        l.allowed_room_designators = { m.rooms[room_idx].designator };
        l.disallowed_room_designators.clear();
      }
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kInfeasible) << r.message;
    }

    TEST(SolverHard, LockedLessonStaysPut) {
      SchoolModel m = TestSchool();
      // Pin the first maths lesson to Wednesday period 2 in room R1.
      m.lessons[0].locked = true;
      m.lessons[0].locked_placement = Placement{ 3, 2, 70 };
      const LessonInstance* locked = &m.lessons[0];
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible);
      bool found = false;
      for (const auto& sl : r.best.lessons) {
        if (sl.lesson_id == locked->id) {
          found = true;
          EXPECT_EQ(sl.placement, *locked->locked_placement);
        }
      }
      EXPECT_TRUE(found);
    }

    TEST(SolverHard, ParallelBlockMembersStartTogether) {
      SchoolModel m = TestSchool();
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible);
      std::map<Id, Placement> placed;
      for (const auto& sl : r.best.lessons) placed[sl.lesson_id] = sl.placement;
      ModelIndex ix = ModelIndex::Build(m);
      ASSERT_FALSE(ix.ParallelBlocks().empty());
      for (const auto& block : ix.ParallelBlocks()) {
        const Placement& first = placed.at(m.lessons[block[0]].id);
        for (int li : block) {
          const Placement& p = placed.at(m.lessons[li].id);
          EXPECT_EQ(p.day_id, first.day_id);
          EXPECT_EQ(p.start_period, first.start_period);
        }
      }
    }

    TEST(SolverHard, TeacherUnavailabilityRespected) {
      SchoolModel m = TestSchool();
      // TA (both divisions' maths) loses Monday's first three periods.
      m.external_blocks = { {900, "rada", BlockTarget::kTeacher, 40, 1, 0, 3} };
      const ExternalBlock& blk = m.external_blocks[0];
      ASSERT_EQ(blk.target, BlockTarget::kTeacher);
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible);
      for (const auto& sl : r.best.lessons) {
        ModelIndex ix = ModelIndex::Build(m);
        const LessonInstance& l = m.lessons[ix.LessonIdx(sl.lesson_id)];
        if (!l.requires_teacher || l.teacher_id != blk.target_id) continue;
        if (sl.placement.day_id != blk.day_id) continue;
        bool overlap =
          sl.placement.start_period < blk.start_period + blk.duration &&
          blk.start_period < sl.placement.start_period + l.duration;
        EXPECT_FALSE(overlap) << "lesson " << l.id;
      }
    }

    TEST(SolverHard, PresetCancellationReturnsCancelled) {
      SchoolModel m = TestSchool();
      std::atomic<bool> cancel{ true };
      SolveResult r = SolveSchedule(m, TestParams(60.0), &cancel, nullptr);
      EXPECT_EQ(r.status, SolveStatusCode::kCancelled);
      EXPECT_LT(r.wall_time_seconds, 5.0);
    }

    TEST(SolverHard, StreamsIntermediateSolutions) {
      SchoolModel m = TestSchool();
      int calls = 0;
      SolveResult r = SolveSchedule(m, TestParams(), nullptr,
        [&] (const SolveResult& intermediate) {
          ++calls;
          EXPECT_FALSE(intermediate.best.lessons.empty());
        });
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible);
      EXPECT_GE(calls, 1);
    }

  }  // namespace
}  // namespace arrango
