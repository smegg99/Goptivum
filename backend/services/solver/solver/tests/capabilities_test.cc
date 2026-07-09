// tests/capabilities_test.cc

#include <gtest/gtest.h>

#include <set>
#include <string>

#include "score/rules.h"
#include "score/verdict.h"
#include "solve/solver.h"
#include "test_school.h"
#include "validate/validator.h"

// THE CAPABILITY INVENTORY — one place mapping every solver capability
// (docs/ROADMAP.md rows) to the test that proves it. When a capability
// ships, add its row here; when a row has no test, that is a bug.
//
//   Calendar/years/priorities .... solver_soft: YearPriorityGetsBetterSlots
//   Splits open/fixed rulebook ... solver_hard: *Split*/*Parallel* (6 tests)
//   Merged/parallel blocks ....... solver_hard: ParallelBlockMembersStartTogether
//   Multi-period lessons ......... solver_hard: BlockShapeRecipe (duration 2)
//   Locked lessons ............... solver_hard: LockedLessonStaysPut
//   External blocks (4 targets) .. solver_hard: SolvesCleanlyWithAllBlockKinds
//   Room designators/fixed/cap ... candidates_test: FixedRoom*, Capacity*
//   Daily load hard min/max ...... daily_load_test + validator_test
//   Daily load soft band ......... solver_soft: ObjectiveMatchesScorerPenaltyWithDailyLoadRule
//   Preferences (early/max) ...... solver_soft: ExtraSubjectExemptFromDailyMax
//   Rule modes hard/soft/off ..... solver_hard: HardSubjectOnce*, HardStudentGaps*;
//                                  scorer_test: RuleModesChangeScoringNotObservation
//   Per-entity overrides ......... scorer_test: TeacherScopedOverrides; rules_test
//   Profiles in core ............. solver_soft: DobryPlanProfileSolvesTinyPristinely
//   Config-hard verdict/baseline . verdict_test: ConfigHardViolationIsErrorTier;
//                                  service_test: HardRuleViolatingBaselineIsRejected
//   teach_daily / few_days ....... solver_soft: TeachDailyAndFewDaysShapeTeacherWeeks
//   anti_split_shift ............. solver_soft: AntiSplitShiftHardWithCatechistExemption
//   single_lesson_day ............ solver_soft: SingleLessonDayWeightConsolidatesDays
//   Lesson links (3 kinds) ....... solver_hard: LessonLinksShapePlacements
//   Edge-of-day .................. solver_hard: EdgePlacementConstrainsStudentDays
//   Pattern recipes .............. solver_hard: BlockShapeRecipe
//   Absolute rating / metrics .... metrics_test + scorer_test
//   Verdict tiers + hygiene ...... verdict_test (7 tests)
//   Preflight named culprits ..... preflight_test (10+ tests)
//   Infeasibility explainer ...... explain_test (5) + service_test:
//                                  InfeasibleSolveStreamsExplanation
//   Timeout hints ................ explain_test: TimeoutHintsNameHeavyInputs
//   Live progress/stages ......... service_test: SolveStreamsStageProgressAndVerdict
//   Heartbeat .................... solver_soft: HeartbeatReportsProgress
//   Warm start/stability ......... solver_soft: StabilityKeepsPreviousPlacements
//   Never-worse baseline ......... service_test: HardRuleViolatingBaselineIsRejected
//   LNS safety (splits/links) .... lns_test + lns link closure
//   Cross-audit .................. service_test: AuditCatchesCorruptedResults
//   Seeds/workers/cancellation ... solver_hard: PresetCancellationReturnsCancelled
//   Stream cap ................... solver_hard: StreamCapRefusesPathologicalFixedSplits
//   Demo presets (prod + mega) ... demo_data_test (4 tests)
//   Drift guard (obj == scorer) .. solver_soft: ObjectiveMatchesScorerPenalty*
//                                  (+ every rule-mode test asserts it)

namespace arrango {
  namespace {

    // One end-to-end pass over the ENTIRE reporting surface: a solved school
    // must come back with a complete metric table, composed verdict, per-entity
    // breakdowns, hygiene container, and the drift guarantee — all at once.
    TEST(Capabilities, FullReportingSurfaceOnOneSolve) {
      SchoolModel m = TestSchool();
      SolveParams params{ .max_time_seconds = 15.0, .num_workers = 1,
                         .random_seed = 7 };
      SolveResult r = SolveSchedule(m, params, nullptr, nullptr);
      ASSERT_EQ(r.status, SolveStatusCode::kOptimal) << r.message;

      ValidationReport validation = Validate(m, r.best);
      EXPECT_TRUE(validation.valid);
      ScoreReport score = ComputeScore(m, r.best);
      EXPECT_EQ(r.objective, score.total_penalty);  // drift guard

      // Every metric of the rating table reports (applicable or not).
      std::set<std::string> reported;
      for (const MetricScore& metric : score.metric_scores) {
        reported.insert(metric.key);
        EXPECT_GE(metric.subscore, 0.0);
        EXPECT_LE(metric.subscore, 100.0);
      }
      for (const char* key : { "gaps", "gap_windows", "lateness", "repeats",
                              "load_band", "overload", "t_gaps", "room_moves",
                              "t_late", "single_days" }) {
        EXPECT_TRUE(reported.count(key)) << key;
      }
      EXPECT_GT(score.overall_quality, 0.0);
      EXPECT_EQ(score.division_scores.size(), m.divisions.size());
      EXPECT_EQ(score.teacher_scores.size(), m.teachers.size());
      EXPECT_EQ(score.year_scores.size(), m.years.size());

      ScheduleVerdict verdict = ComposeVerdict(validation, score);
      EXPECT_EQ(verdict.errors, 0u);
      EXPECT_NE(verdict.tier, VerdictTier::kErrors);
    }

    // Every shipped profile resolves cleanly against a real model, and the
    // rule table agrees with itself (every category some rule claims exists
    // exactly once across rules).
    TEST(Capabilities, ProfilesAndRuleTableAreCoherent) {
      SchoolModel m = TestSchool();
      ModelIndex ix = ModelIndex::Build(m);
      for (const char* profile : { "", "default", "dobry_plan", "relaxed" }) {
        m.rule_config.profile = profile;
        RuleResolver resolver = RuleResolver::Build(m, ix, {});
        EXPECT_TRUE(resolver.Diagnostics().empty()) << profile;
      }
      std::set<std::string> categories;
      for (const RuleDef& rule : RuleTable()) {
        for (const char* category : rule.categories) {
          EXPECT_TRUE(categories.insert(category).second)
            << "category claimed twice: " << category;
        }
      }
    }

  }  // namespace
}  // namespace arrango
