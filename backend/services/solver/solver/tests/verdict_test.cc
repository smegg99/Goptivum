// tests/verdict_test.cc

#include <gtest/gtest.h>

#include <algorithm>

#include "score/verdict.h"
#include "validate/validator.h"

namespace arrango {
  namespace {

    // One day x 6 periods, one division, one teacher, roomless lessons — small
    // enough to steer every tier deliberately.
    SchoolModel M() {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      for (auto [id, subject] : { std::pair<Id, Id>{100, 50}, {101, 51} }) {
        m.lessons.push_back({ .id = id, .participants = {{20, kNoId}},
                             .subject_id = subject, .teacher_id = 40,
                             .requires_room = false });
      }
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      return m;
    }

    ScheduleVerdict VerdictOf(const SchoolModel& m, const ScheduleSnapshot& s) {
      return ComposeVerdict(Validate(m, s), ComputeScore(m, s));
    }

    TEST(Verdict, CleanScheduleIsPristine) {
      SchoolModel m = M();
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}}, {101, {1, 1, kNoId}}} };
      ScheduleVerdict v = VerdictOf(m, s);
      EXPECT_EQ(v.tier, VerdictTier::kPristine);
      EXPECT_EQ(v.errors, 0u);
      EXPECT_EQ(v.warnings, 0u);
    }

    TEST(Verdict, StudentGapIsAWarning) {
      SchoolModel m = M();
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}}, {101, {1, 2, kNoId}}} };  // gap p1
      ScheduleVerdict v = VerdictOf(m, s);
      EXPECT_EQ(v.tier, VerdictTier::kWarnings);
      EXPECT_GE(v.warnings, 1u);
      EXPECT_EQ(v.errors, 0u);
    }

    TEST(Verdict, HardConflictIsErrorsRegardlessOfWarnings) {
      SchoolModel m = M();
      // Both whole-class lessons at the same slot: teacher + student conflicts.
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}}, {101, {1, 0, kNoId}}} };
      ScheduleVerdict v = VerdictOf(m, s);
      EXPECT_EQ(v.tier, VerdictTier::kErrors);
      EXPECT_GE(v.errors, 1u);
    }

    // A violation of a rule the school set to HARD is an ERROR — counted apart
    // from structural illegality — even though the schedule is legal.
    TEST(Verdict, ConfigHardViolationIsErrorTier) {
      SchoolModel m = M();
      m.lessons.push_back({ .id = 102, .participants = {{20, kNoId}},
                           .subject_id = 50, .teacher_id = 40,
                           .requires_room = false });
      m.rule_config.overrides = { {"subject_once", RuleMode::kHard} };
      // Contiguous day, but subject 50 runs twice (periods 0 and 2).
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}},
                          {101, {1, 1, kNoId}},
                          {102, {1, 2, kNoId}}} };
      ValidationReport validation = Validate(m, s);
      EXPECT_TRUE(validation.valid);  // structurally legal
      ScheduleVerdict v = ComposeVerdict(validation, ComputeScore(m, s));
      EXPECT_EQ(v.tier, VerdictTier::kErrors);
      EXPECT_GE(v.config_hard_errors, 1u);
      EXPECT_EQ(v.errors, v.config_hard_errors);  // no structural errors
      EXPECT_EQ(v.warnings, 0u);
    }

    // Preference-flavored issues (a moved lesson vs its previous placement)
    // never block pristine — the bar is comfort, not stability.
    TEST(Verdict, StabilityMoveIsNotAWarning) {
      SchoolModel m = M();
      m.lessons[0].previous_placement = Placement{ 1, 3, kNoId };  // will move
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}}, {101, {1, 1, kNoId}}} };
      ScoreReport score = ComputeScore(m, s);
      const bool has_stability = std::any_of(
        score.soft_issues.begin(), score.soft_issues.end(),
        [] (const SoftIssue& i) { return i.category == "stability"; });
      ASSERT_TRUE(has_stability);
      EXPECT_EQ(VerdictOf(m, s).tier, VerdictTier::kPristine);
    }

    // The two derivations of "pristine" can never disagree: zero warnings iff
    // every applicable designated metric reads exactly 100.
    TEST(Verdict, WarningsMatchMetricSubscores) {
      SchoolModel m = M();
      for (const auto& s : { ScheduleSnapshot{{{100, {1, 0, kNoId}},
                                              {101, {1, 1, kNoId}}}},
                            ScheduleSnapshot{{{100, {1, 0, kNoId}},
                                              {101, {1, 2, kNoId}}}} }) {
        ScoreReport score = ComputeScore(m, s);
        ScheduleVerdict v = ComposeVerdict(Validate(m, s), score);
        const bool all_hundred = std::all_of(
          score.metric_scores.begin(), score.metric_scores.end(),
          [] (const MetricScore& ms) {
            return !ms.applicable || ms.subscore >= 100.0 - 1e-9;
          });
        EXPECT_EQ(v.warnings == 0, all_hundred);
      }
    }

    // Hygiene detectors land in info_issues, count as infos, and leave both the
    // quality and the pristine tier untouched.
    TEST(Verdict, HygieneInfoNeverBlocksPristine) {
      SchoolModel m = M();
      // Day starts at period 4 (> kInfoLatestFirstLesson): contiguous, no gaps,
      // but a "late first lesson" hygiene finding.
      ScheduleSnapshot s{ {{100, {1, 4, kNoId}}, {101, {1, 5, kNoId}}} };
      ScoreReport score = ComputeScore(m, s);
      const bool has_late_first = std::any_of(
        score.info_issues.begin(), score.info_issues.end(),
        [] (const SoftIssue& i) { return i.category == "late_first_lesson"; });
      EXPECT_TRUE(has_late_first);
      ScheduleVerdict v = ComposeVerdict(Validate(m, s), score);
      EXPECT_GE(v.infos, 1u);
      // Lateness metric: threshold is period 7, so periods 4-5 stay clean.
      EXPECT_EQ(v.tier, VerdictTier::kPristine);
      EXPECT_DOUBLE_EQ(score.all_students_quality, 100.0);
    }

    TEST(Verdict, StartVarianceAndAlwaysLastDetected) {
      SchoolModel m = M();
      m.days.push_back({ 2, "Wt", 6 });
      // Two more lessons so both days are used; subject 51 sits last both days.
      m.lessons.push_back({ .id = 102, .participants = {{20, kNoId}},
                           .subject_id = 50, .teacher_id = 40,
                           .requires_room = false });
      m.lessons.push_back({ .id = 103, .participants = {{20, kNoId}},
                           .subject_id = 51, .teacher_id = 40,
                           .requires_room = false });
      // Mon: p0 (Mat), p1 (Ang=last... no: Ang at p1, Mat at p0) — make Ang last
      // on both days and start Monday p0 vs Tuesday p4 (variance 4 > 2).
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}},
                          {101, {1, 1, kNoId}},   // Ang last on Monday
                          {102, {2, 4, kNoId}},
                          {103, {2, 5, kNoId}}} };  // Ang last on Tuesday
      ScoreReport score = ComputeScore(m, s);
      auto has = [&] (const char* cat) {
        return std::any_of(score.info_issues.begin(), score.info_issues.end(),
          [cat] (const SoftIssue& i) { return i.category == cat; });
        };
      EXPECT_TRUE(has("start_variance"));
      EXPECT_TRUE(has("subject_always_last"));
    }

  }  // namespace
}  // namespace arrango
