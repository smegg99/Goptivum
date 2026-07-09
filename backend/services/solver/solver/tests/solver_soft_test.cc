// tests/solver_soft_test.cc

#include <gtest/gtest.h>

#include <algorithm>
#include <atomic>
#include <map>
#include <set>
#include <vector>

#include "demo/demo_data.h"
#include "test_school.h"
#include "score/scorer.h"
#include "solve/solver.h"
#include "validate/validator.h"

namespace arrango {
  namespace {

    SolveParams TestParams(double seconds = 10.0) {
      return { .max_time_seconds = seconds, .num_workers = 1, .random_seed = 7 };
    }

    const PenaltyItem* FindItem(const std::vector<PenaltyItem>& items,
      const std::string& cat) {
      for (const auto& i : items) {
        if (i.category == cat) return &i;
      }
      return nullptr;
    }

    // One class, one teacher, one room, 3 single-period lessons of distinct
    // subjects in a 6-period day: a gap-free layout exists and must be found.
    TEST(SolverSoft, AvoidsStudentGapsWhenPossible) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "T"} };
      m.subjects = { {50, "A"}, {51, "B"}, {52, "C"} };
      m.rooms = { {70, "R", "R"} };
      for (Id i = 0; i < 3; ++i) {
        m.lessons.push_back({ .id = 100 + i, .participants = {{20, kNoId}},
                             .subject_id = 50 + i, .teacher_id = 40 });
      }
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      ScoreReport score = ComputeScore(m, r.best);
      EXPECT_EQ(FindItem(score.global_items, "student_gap"), nullptr);
      EXPECT_EQ(FindItem(score.global_items, "teacher_gap"), nullptr);
      EXPECT_EQ(r.objective, 0);
    }

    // One shared room open only late in the day (p6..p9). Late costs scale with
    // year priority, so the year-1 class must get the earlier slots.
    TEST(SolverSoft, YearPriorityGetsBetterSlots) {
      SchoolModel m;
      m.days = { {1, "Pon", 10} };
      for (Id p = 1; p <= 10; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300}, {11, "Rok 3", 3, 100} };
      m.divisions = { {20, "1A", 10}, {21, "3A", 11} };
      m.teachers = { {40, "T1"}, {41, "T3"} };
      m.subjects = { {50, "A"}, {51, "B"} };
      m.rooms = { {70, "R", "R"} };
      m.external_blocks = { {90, "remont", BlockTarget::kRoom, 70, 1, 0, 6} };
      int i = 0;
      for (Id cls : {Id{ 20 }, Id{ 21 }}) {
        for (Id subj : {Id{ 50 }, Id{ 51 }}) {
          m.lessons.push_back({ .id = static_cast<Id>(100 + i++),
                               .participants = {{cls, kNoId}},
                               .subject_id = subj,
                               .teacher_id = cls == 20 ? Id{40} : Id{41} });
        }
      }
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      std::map<Id, uint32_t> start;
      for (const auto& sl : r.best.lessons) {
        start[sl.lesson_id] = sl.placement.start_period;
      }
      // Year-1 lessons (ids 100,101) take p6,p7; year-3 (102,103) take p8,p9.
      EXPECT_LT(std::max(start[100], start[101]),
        std::min(start[102], start[103]));
    }

    TEST(SolverSoft, StabilityKeepsPreviousPlacements) {
      SchoolModel m = TestSchool();
      SolveResult first = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(first.status, SolveStatusCode::kOptimal) << first.message;
      std::map<Id, Placement> placed;
      for (const auto& sl : first.best.lessons) {
        placed[sl.lesson_id] = sl.placement;
      }
      for (auto& l : m.lessons) {
        l.previous_placement = placed.at(l.id);
      }
      SolveResult second = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(second.status, SolveStatusCode::kOptimal) << second.message;
      for (const auto& sl : second.best.lessons) {
        EXPECT_EQ(sl.placement, placed.at(sl.lesson_id))
          << "lesson " << sl.lesson_id << " moved";
      }
    }

    // disable_warm_start skips only the CP-SAT hints; the stability penalty
    // still pulls toward previous placements, so the optimum is unchanged.
    TEST(SolverSoft, DisableWarmStartStillSolvesOptimallyWithStability) {
      SchoolModel m = TestSchool();
      SolveResult first = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(first.status, SolveStatusCode::kOptimal) << first.message;
      std::map<Id, Placement> placed;
      for (const auto& sl : first.best.lessons) {
        placed[sl.lesson_id] = sl.placement;
      }
      for (auto& l : m.lessons) {
        l.previous_placement = placed.at(l.id);
      }
      SolveParams params = TestParams();
      params.disable_warm_start = true;
      SolveResult second = SolveSchedule(m, params, nullptr, nullptr);
      ASSERT_EQ(second.status, SolveStatusCode::kOptimal) << second.message;
      for (const auto& sl : second.best.lessons) {
        EXPECT_EQ(sl.placement, placed.at(sl.lesson_id))
          << "lesson " << sl.lesson_id << " moved";
      }
    }

    // A room outage in the middle of the day: taking the early slots would
    // leave a student gap at the blocked period. The solver must shift the
    // whole day later (contiguous) instead of accepting the gap.
    TEST(SolverSoft, PrefersShiftedDayOverStudentGap) {
      SchoolModel m;
      m.days = { {1, "Pon", 10} };
      for (Id p = 1; p <= 10; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "T"} };
      m.subjects = { {50, "A"}, {51, "B"}, {52, "C"} };
      m.rooms = { {70, "R", "R"} };
      m.external_blocks = { {90, "remont", BlockTarget::kRoom, 70, 1, 2, 1} };
      for (Id i = 0; i < 3; ++i) {
        m.lessons.push_back({ .id = 100 + i, .participants = {{20, kNoId}},
                             .subject_id = 50 + i, .teacher_id = 40 });
      }
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      ScoreReport score = ComputeScore(m, r.best);
      EXPECT_EQ(FindItem(score.global_items, "student_gap"), nullptr);
      // All three lessons contiguous and clear of the blocked period 2.
      std::vector<uint32_t> starts;
      for (const auto& sl : r.best.lessons) {
        starts.push_back(sl.placement.start_period);
        EXPECT_NE(sl.placement.start_period, 2u);
      }
      std::sort(starts.begin(), starts.end());
      EXPECT_EQ(starts[1], starts[0] + 1);
      EXPECT_EQ(starts[2], starts[1] + 1);
    }

    TEST(SolverSoft, HeartbeatReportsProgress) {
      SchoolModel m = GenerateDemoSchool(DemoPreset::kProduction, 42);
      std::atomic<int> ticks{ 0 };
      int64_t last_bound = -1, last_objective = -1;
      SolveResult r = SolveSchedule(
        m, TestParams(5.0), nullptr, nullptr,
        [&] (const SolveResult& tick) {
          ++ticks;
          last_bound = tick.best_bound;
          last_objective = tick.objective;
        });
      EXPECT_GE(ticks.load(), 3);  // ~1 Hz over 5 seconds
      if (last_objective > 0) {
        EXPECT_LE(last_bound, last_objective);
      }
      EXPECT_GE(r.best_bound, 0);
    }

    // Student gap windows of >= 2 consecutive periods are a HARD constraint:
    // two lessons locked with a 2-period hole between them are infeasible; a
    // 1-period hole stays feasible (and expensive).
    TEST(SolverSoft, DoubleGapWindowIsInfeasible) {
      auto build = [] (uint32_t second_start) {
        SchoolModel m;
        m.days = { {1, "Pon", 6} };
        for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
        m.years = { {10, "Rok 1", 1, 300} };
        m.divisions = { {20, "1A", 10} };
        m.teachers = { {40, "T"}, {41, "U"} };
        m.subjects = { {50, "A"}, {51, "B"} };
        m.rooms = { {70, "R", "R"}, {71, "S", "S"} };
        m.lessons = {
            {.id = 100, .participants = {{20, kNoId}}, .subject_id = 50,
             .teacher_id = 40, .allowed_room_designators = {"R"}, .locked = true,
             .locked_placement = Placement{1, 0, 70}},
            {.id = 101, .participants = {{20, kNoId}}, .subject_id = 51,
             .teacher_id = 41, .allowed_room_designators = {"S"}, .locked = true,
             .locked_placement = Placement{1, second_start, 71}},
        };
        return m;
        };
      SolveResult infeasible =
        SolveSchedule(build(3), TestParams(), nullptr, nullptr);
      EXPECT_EQ(infeasible.status, SolveStatusCode::kInfeasible)
        << "p0 + p3 locked leaves a 2-period window (p1,p2)";

      SolveResult feasible =
        SolveSchedule(build(2), TestParams(), nullptr, nullptr);
      ASSERT_EQ(feasible.status, SolveStatusCode::kOptimal) << feasible.message;
      ScoreReport score = ComputeScore(build(2), feasible.best);
      const PenaltyItem* gap = FindItem(score.global_items, "student_gap");
      ASSERT_NE(gap, nullptr);  // the single-period window is soft-penalized
      EXPECT_EQ(gap->count, 1u);
    }

    // The same forced 2-period window becomes legal when the school softens
    // gap_windows — and the priced pair keeps objective == scorer penalty
    // (drift guard under a non-default mode).
    TEST(SolverSoft, SoftGapWindowIsPricedNotForbidden) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "T"}, {41, "U"} };
      m.subjects = { {50, "A"}, {51, "B"} };
      m.rooms = { {70, "R", "R"}, {71, "S", "S"} };
      m.lessons = {
          {.id = 100, .participants = {{20, kNoId}}, .subject_id = 50,
           .teacher_id = 40, .allowed_room_designators = {"R"}, .locked = true,
           .locked_placement = Placement{1, 0, 70}},
          {.id = 101, .participants = {{20, kNoId}}, .subject_id = 51,
           .teacher_id = 41, .allowed_room_designators = {"S"}, .locked = true,
           .locked_placement = Placement{1, 3, 71}},
      };
      m.rule_config.overrides = { {"gap_windows", RuleMode::kSoft} };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      ScoreReport score = ComputeScore(m, r.best);
      const PenaltyItem* window = FindItem(score.global_items, "gap_window");
      ASSERT_NE(window, nullptr);
      EXPECT_EQ(window->count, 1u);  // one adjacent pair (p1,p2)
      EXPECT_GT(window->penalty, 0);
      EXPECT_EQ(r.objective, score.total_penalty);  // drift guard
    }

    // Drift guard under a whole non-default profile plus a teacher override.
    TEST(SolverSoft, ObjectiveMatchesScorerPenaltyUnderRelaxedProfile) {
      SchoolModel m = TestSchool();
      m.rule_config.profile = "relaxed";
      m.rule_config.overrides = {
          {"teacher_gaps", RuleMode::kSoft, /*weight=*/450, 0, kNoId, kNoId,
           kNoId, m.teachers.front().id},
          {"room_changes", RuleMode::kOff, 0, 0, kNoId, kNoId, kNoId,
           m.teachers.back().id} };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible)
        << r.message;
      EXPECT_EQ(r.objective, ComputeScore(m, r.best).total_penalty);
    }

    // A weighted single_lesson_day rule pushes the solver to consolidate: three
    // lessons across two days land 3+0, never 2+1 (a lonely day costs 100).
    TEST(SolverSoft, SingleLessonDayWeightConsolidatesDays) {
      SchoolModel m;
      m.days = { {1, "Pon", 3}, {2, "Wt", 3} };
      for (Id p = 1; p <= 3; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"}, {52, "His"} };
      for (auto [id, subject] :
        { std::pair<Id, Id>{100, 50}, {101, 51}, {102, 52} }) {
        m.lessons.push_back({ .id = id, .participants = {{20, kNoId}},
                             .subject_id = subject, .teacher_id = 40,
                             .requires_room = false });
      }
      // Disable the built-in daily minimum: consolidation needs an empty day.
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      m.rule_config.overrides = {
          {"single_lesson_day", RuleMode::kSoft, /*weight=*/100} };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      std::map<Id, int> per_day;
      for (const auto& sl : r.best.lessons) ++per_day[sl.placement.day_id];
      for (const auto& [day, count] : per_day) EXPECT_NE(count, 1);
      EXPECT_EQ(r.objective, ComputeScore(m, r.best).total_penalty);
    }

    // anti_split_shift: lessons locked at both day edges are illegal under the
    // hard rule — unless the teacher is exempted (the catechist override).
    TEST(SolverSoft, AntiSplitShiftHardWithCatechistExemption) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10}, {21, "1B", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Rel"} };
      // Locked at period 0 (early edge) and period 5 (late edge), different
      // divisions so students have no forced gap.
      for (auto [id, division, period] :
        { std::tuple<Id, Id, uint32_t>{100, 20, 0}, {101, 21, 5} }) {
        LessonInstance l{ .id = id, .participants = {{division, kNoId}},
                         .subject_id = 50, .teacher_id = 40,
                         .requires_room = false };
        l.locked = true;
        l.locked_placement = Placement{ 1, period, kNoId };
        m.lessons.push_back(l);
      }
      m.rule_config.overrides = { {"anti_split_shift", RuleMode::kHard} };
      SolveResult hard = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_EQ(hard.status, SolveStatusCode::kInfeasible);

      // Catechists legitimately teach at both edges: exempt this teacher.
      m.rule_config.overrides.push_back(
        { "anti_split_shift", RuleMode::kOff, 0, 0, kNoId, kNoId, kNoId,
        /*teacher=*/40 });
      SolveResult exempt = SolveSchedule(m, TestParams(), nullptr, nullptr);
      EXPECT_TRUE(exempt.status == SolveStatusCode::kOptimal ||
        exempt.status == SolveStatusCode::kFeasible);

      // Soft mode prices the same shape and stays drift-exact.
      m.rule_config.overrides = {
          {"anti_split_shift", RuleMode::kSoft, /*weight=*/60} };
      SolveResult soft = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(soft.status, SolveStatusCode::kOptimal) << soft.message;
      ScoreReport score = ComputeScore(m, soft.best);
      const PenaltyItem* shift = FindItem(score.global_items, "anti_split_shift");
      ASSERT_NE(shift, nullptr);
      EXPECT_EQ(shift->penalty, 60);
      EXPECT_EQ(soft.objective, score.total_penalty);
    }

    // The dobry_plan profile (their iron invariants: gaps hard, windows hard,
    // subject-once hard, anti-split-shift on) must be satisfiable on a real
    // model — and its hard rules leave literally zero violations behind.
    TEST(SolverSoft, DobryPlanProfileSolvesTinyPristinely) {
      SchoolModel m = TestSchool();
      m.rule_config.profile = "dobry_plan";
      SolveResult r = SolveSchedule(m, TestParams(20.0), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible)
        << r.message;
      ScoreReport score = ComputeScore(m, r.best);
      for (const char* category : { "student_gap", "gap_window", "subject_split",
                                   "block_break", "anti_split_shift" }) {
        const PenaltyItem* item = FindItem(score.global_items, category);
        EXPECT_TRUE(item == nullptr || item->count == 0) << category;
      }
      EXPECT_EQ(r.objective, score.total_penalty);
    }

    // teach_daily ("codziennie") pushes a teacher's lessons onto every day;
    // few_days ("celebryta") pulls them together — both drift-exact.
    TEST(SolverSoft, TeachDailyAndFewDaysShapeTeacherWeeks) {
      SchoolModel m;
      m.days = { {1, "Pon", 3}, {2, "Wt", 3} };
      for (Id p = 1; p <= 3; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      for (auto [id, subject] : { std::pair<Id, Id>{100, 50}, {101, 51} }) {
        m.lessons.push_back({ .id = id, .participants = {{20, kNoId}},
                             .subject_id = subject, .teacher_id = 40,
                             .requires_room = false });
      }
      auto active_days = [] (const SolveResult& r) {
        std::set<Id> days;
        for (const auto& sl : r.best.lessons) days.insert(sl.placement.day_id);
        return days.size();
        };

      SchoolModel daily = m;
      daily.rule_config.overrides = {
          {"teach_daily", RuleMode::kSoft, /*weight=*/100, 0, kNoId, kNoId,
           kNoId, /*teacher=*/40} };
      SolveResult spread = SolveSchedule(daily, TestParams(), nullptr, nullptr);
      ASSERT_EQ(spread.status, SolveStatusCode::kOptimal) << spread.message;
      EXPECT_EQ(active_days(spread), 2u);
      EXPECT_EQ(spread.objective, ComputeScore(daily, spread.best).total_penalty);

      SchoolModel few = m;
      few.rule_config.overrides = {
          {"few_days", RuleMode::kSoft, /*weight=*/100, /*param=*/1, kNoId,
           kNoId, kNoId, /*teacher=*/40} };
      SolveResult packed = SolveSchedule(few, TestParams(), nullptr, nullptr);
      ASSERT_EQ(packed.status, SolveStatusCode::kOptimal) << packed.message;
      EXPECT_EQ(active_days(packed), 1u);
      EXPECT_EQ(packed.objective, ComputeScore(few, packed.best).total_penalty);

      // Hard variants: forced spread / forced packing.
      few.rule_config.overrides[0].mode = RuleMode::kHard;
      few.rule_config.overrides[0].weight = 0;
      SolveResult hard_packed = SolveSchedule(few, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(hard_packed.status == SolveStatusCode::kOptimal ||
        hard_packed.status == SolveStatusCode::kFeasible);
      EXPECT_EQ(active_days(hard_packed), 1u);
    }

    // "Przedmiot extra": a subject-scoped OFF override on max_lessons exempts
    // that subject's periods from the daily count — hard mode included.
    TEST(SolverSoft, ExtraSubjectExemptFromDailyMax) {
      SchoolModel m;
      m.days = { {1, "Pon", 5} };
      for (Id p = 1; p <= 5; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"}, {52, "His"}, {53, "WF"} };
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      for (auto [id, subject] :
        { std::pair<Id, Id>{100, 50}, {101, 51}, {102, 52}, {103, 53} }) {
        m.lessons.push_back({ .id = id, .participants = {{20, kNoId}},
                             .subject_id = subject, .teacher_id = 40,
                             .requires_room = false });
      }
      m.preferences = { {PreferenceKind::kMaxLessonsPerDay, kNoId, 20, kNoId,
        /*weight=*/50, /*param=*/3} };

      // Hard, no exemption: 4 lessons on the only day can never fit max 3.
      m.rule_config.overrides = { {"max_lessons", RuleMode::kHard} };
      EXPECT_EQ(SolveSchedule(m, TestParams(), nullptr, nullptr).status,
        SolveStatusCode::kInfeasible);

      // Hard + WF exempt: legal, and the soft variant prices zero — drift-safe.
      m.rule_config.overrides.push_back(
        { "max_lessons", RuleMode::kOff, 0, 0, kNoId, kNoId, /*subject=*/53 });
      SolveResult exempt = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(exempt.status == SolveStatusCode::kOptimal ||
        exempt.status == SolveStatusCode::kFeasible)
        << exempt.message;
      ScoreReport score = ComputeScore(m, exempt.best);
      const PenaltyItem* max_item = FindItem(score.global_items, "max_lessons");
      EXPECT_TRUE(max_item == nullptr || max_item->count == 0);
      EXPECT_EQ(exempt.objective, score.total_penalty);

      // Soft without exemption: exactly one counted excess period.
      m.rule_config.overrides = {};
      SolveResult soft = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(soft.status, SolveStatusCode::kOptimal) << soft.message;
      ScoreReport soft_score = ComputeScore(m, soft.best);
      const PenaltyItem* item = FindItem(soft_score.global_items, "max_lessons");
      ASSERT_NE(item, nullptr);
      EXPECT_EQ(item->count, 1u);
      EXPECT_EQ(soft.objective, soft_score.total_penalty);
    }

    // Guards the scorer and the CP-SAT objective against drifting apart.
    TEST(SolverSoft, ObjectiveMatchesScorerPenalty) {
      {
        SchoolModel m = TestSchool();
        SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
        ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
          r.status == SolveStatusCode::kFeasible)
          << r.message;
        ScoreReport score = ComputeScore(m, r.best);
        EXPECT_EQ(r.objective, score.total_penalty);
      }
    }

    // Drift guard with a FIXED split in play: the scorer's product streams and
    // the objective's stream terms must still agree exactly.
    TEST(SolverSoft, ObjectiveMatchesScorerPenaltyWithFixedSplit) {
      SchoolModel m = TestSchool();
      const Id division = m.divisions[0].id;
      m.teachers.push_back({ 990, "WFista", "" });
      m.splits.push_back({ 980, "wf", division, SplitKind::kFixed });
      m.groups.push_back(
        { .id = 981, .name = "dz", .division_id = division, .split_id = 980 });
      m.groups.push_back(
        { .id = 982, .name = "ch", .division_id = division, .split_id = 980 });
      for (Id group : {Id{ 981 }, Id{ 982 }}) {
        LessonInstance l;
        l.id = 900 + group;
        l.participants = { {division, group} };
        l.subject_id = m.subjects[0].id;
        l.teacher_id = 990;
        l.requires_room = false;
        m.lessons.push_back(l);
      }
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible)
        << r.message;
      EXPECT_EQ(r.objective, ComputeScore(m, r.best).total_penalty);
    }

    // Dobry Plan's religion pattern: one group of a split has lessons, the
    // sibling group none. The sibling stream's gap penalty must force the
    // group lesson to a day edge — no dedicated constraint exists for this.
    TEST(SolverSoft, PartiallyUsedSplitLessonGoesToDayEdge) {
      SchoolModel m;
      m.days = { {1, "Pon", 5} };
      for (Id p = 1; p <= 5; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"}, {41, "Katecheta"} };
      m.subjects = { {50, "Mat"}, {51, "Religia"} };
      m.splits = { {5, "religia", 20, SplitKind::kOpen} };
      m.groups = { {.id = 30, .name = "rel", .division_id = 20, .split_id = 5},
                  {.id = 31, .name = "brak", .division_id = 20, .split_id = 5} };
      for (Id id : {Id{ 100 }, Id{ 101 }, Id{ 102 }}) {  // whole-class core lessons
        LessonInstance l;
        l.id = id;
        l.participants = { {20, kNoId} };
        l.subject_id = 50;
        l.teacher_id = 40;
        l.requires_room = false;
        m.lessons.push_back(l);
      }
      LessonInstance religion;
      religion.id = 110;
      religion.participants = { {20, 30} };
      religion.subject_id = 51;
      religion.teacher_id = 41;
      religion.requires_room = false;
      m.lessons.push_back(religion);

      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      // Mid-day religion would give the "brak" stream a gap; the optimum has none.
      ScoreReport score = ComputeScore(m, r.best);
      for (const PenaltyItem& item : score.global_items) {
        EXPECT_NE(item.category, "student_gap") << "penalty " << item.penalty;
      }
    }

    // Same drift guard with every soft daily-load weight active.
    TEST(SolverSoft, ObjectiveMatchesScorerPenaltyWithDailyLoadRule) {
      SchoolModel m = TestSchool();
      m.daily_load_rules.push_back({ kNoId, kNoId, /*min=*/0, /*max=*/0,
        /*target=*/3, /*allowed_deviation=*/1,
        /*deviation_weight=*/25,
        /*imbalance_weight=*/40,
        /*overload_weight=*/15,
        /*underload_weight=*/10 });
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible)
        << r.message;
      ScoreReport score = ComputeScore(m, r.best);
      EXPECT_EQ(r.objective, score.total_penalty);
    }

    // Four identical lessons, two days, imbalance-only rule: 2+2 is the unique
    // optimum (4+0 costs 4, 3+1 costs 2, 2+2 costs 0; nothing else differs).
    TEST(SolverSoft, DailyLoadImbalanceBalancesDays) {
      SchoolModel m;
      m.days = { {1, "Pon", 4}, {2, "Wt", 4} };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"} };
      for (Id id = 100; id < 104; ++id) {
        LessonInstance l{ .id = id, .participants = {{20, kNoId}},
                         .subject_id = 50, .teacher_id = 40 };
        l.requires_room = false;
        m.lessons.push_back(l);
      }
      m.daily_load_rules = { {kNoId, kNoId, /*min=*/0, /*max=*/0, /*target=*/2,
        /*allowed_deviation=*/0, /*deviation_weight=*/0,
        /*imbalance_weight=*/500, /*overload_weight=*/0,
        /*underload_weight=*/0} };
      SolveResult r = SolveSchedule(m, TestParams(), nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;
      std::map<Id, int> per_day;
      for (const auto& sl : r.best.lessons) ++per_day[sl.placement.day_id];
      EXPECT_EQ(per_day[1], 2);
      EXPECT_EQ(per_day[2], 2);
      EXPECT_EQ(r.objective, ComputeScore(m, r.best).total_penalty);
    }

    // Mid-size smoke on all cores: the test school with every block kind plus
    // a rule profile stays feasible, valid, and drift-exact.
    TEST(SolverSoft, LoadedTestSchoolSolvesWithinShortLimit) {
      SchoolModel m = TestSchool();
      m.external_blocks = {
          {900, "rada", BlockTarget::kTeacher, 40, /*day=*/1, 0, 2},
          {901, "basen", BlockTarget::kDivision, 21, /*day=*/2, 0, 2} };
      m.rule_config.profile = "relaxed";
      SolveParams params{ .max_time_seconds = 30.0, .num_workers = 0,
                         .random_seed = 7 };
      SolveResult r = SolveSchedule(m, params, nullptr, nullptr);
      ASSERT_TRUE(r.status == SolveStatusCode::kOptimal ||
        r.status == SolveStatusCode::kFeasible)
        << r.message;
      ValidationReport v = Validate(m, r.best);
      EXPECT_TRUE(v.valid) << (v.conflicts.empty() ? ""
        : v.conflicts[0].message);
      ScoreReport score = ComputeScore(m, r.best);
      // No quality floor: near-hard gap weights make the 0-100 quality of a
      // 20s single-worker solve vary too much for a stable threshold. The
      // invariants are feasibility, validity, and the drift guard.
      EXPECT_EQ(r.objective, score.total_penalty);
    }

  }  // namespace
}  // namespace arrango
