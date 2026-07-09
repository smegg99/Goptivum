// tests/capabilities_test.cc

#include <gtest/gtest.h>

#include <set>
#include <string>

#include "score/rules.h"
#include "score/verdict.h"
#include "solve/solver.h"
#include "test_school.h"
#include "validate/validator.h"

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
