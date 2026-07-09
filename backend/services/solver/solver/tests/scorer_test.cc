// tests/scorer_test.cc

#include <gtest/gtest.h>

#include <algorithm>

#include "score/scorer.h"

namespace arrango {
  namespace {

    // 1 day x 10 periods, two group-less divisions: 1A (year 1, priority 300) and
    // 3A (year 3, priority 100). Three 1-period lessons per class, one teacher
    // per class. Late threshold default = period 7.
    constexpr Id kDay = 1, kY1 = 10, kY3 = 11, kC1A = 20, kC3A = 21;
    constexpr Id kTA = 40, kTB = 41, kMat = 50, kSala = 60;
    constexpr Id kR1 = 70, kR2 = 71, kR3 = 72;

    SchoolModel BaseModel() {
      SchoolModel m;
      m.days = { {kDay, "Pon", 10} };
      for (Id p = 1; p <= 10; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {kY1, "Rok 1", 1, 300}, {kY3, "Rok 3", 3, 100} };
      m.divisions = { {kC1A, "1A", kY1}, {kC3A, "3A", kY3} };
      m.teachers = { {kTA, "TA"}, {kTB, "TB"} };
      m.subjects = { {kMat, "Matematyka"} };
      m.rooms = { {kR1, "R1", "R1"}, {kR2, "R2", "R2"}, {kR3, "R3", "R3"} };
      Id next = 100;
      for (Id cls : {kC1A, kC3A}) {
        for (int i = 0; i < 3; ++i) {
          LessonInstance l{ .id = next++, .participants = {{cls, kNoId}},
                           .subject_id = kMat,
                           .teacher_id = cls == kC1A ? kTA : kTB };
          m.lessons.push_back(l);
        }
      }
      return m;
    }

    // Lessons 100..102 (1A) and 103..105 (3A) placed compactly at p0..p2.
    ScheduleSnapshot CompactSnapshot() {
      return { {
          {100, {kDay, 0, kR1}},
          {101, {kDay, 1, kR1}},
          {102, {kDay, 2, kR1}},
          {103, {kDay, 0, kR2}},
          {104, {kDay, 1, kR2}},
          {105, {kDay, 2, kR2}},
      } };
    }

    const EntityScore& ByEntity(const std::vector<EntityScore>& v, Id id) {
      auto it = std::find_if(v.begin(), v.end(), [id] (const EntityScore& e) {
        return e.entity_id == id;
        });
      EXPECT_NE(it, v.end());
      return *it;
    }

    const PenaltyItem* FindItem(const std::vector<PenaltyItem>& items,
      const std::string& cat) {
      auto it = std::find_if(items.begin(), items.end(), [&] (const auto& i) {
        return i.category == cat;
        });
      return it == items.end() ? nullptr : &*it;
    }

    TEST(Scorer, CompactScheduleScoresHigh) {
      ScoreReport r = ComputeScore(BaseModel(), CompactSnapshot());
      EXPECT_GT(r.overall_quality, 80.0);
      EXPECT_EQ(r.total_penalty, 0);
      EXPECT_EQ(FindItem(r.global_items, "student_gap"), nullptr);
    }

    // 2 days x 6 periods, one division (year priority 300), five 1-period
    // lessons. Rule: target 3, allowed_deviation 0, all four weights set.
    // Day 1 holds 4 lessons (p0..p3), day 2 holds 1 (p0):
    //   day 1: overload 1; day 2: underload 2; deviation 1+2; imbalance 4-1.
    TEST(Scorer, DailyLoadSoftPenaltiesAreExact) {
      SchoolModel m;
      m.days = { {1, "Pon", 6}, {2, "Wt", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {kY1, "Rok 1", 1, 300} };
      m.divisions = { {kC1A, "1A", kY1} };
      m.teachers = { {kTA, "TA"} };
      m.subjects = { {kMat, "Matematyka"} };
      m.rooms = { {kR1, "R1", "R1"} };
      for (Id id = 100; id < 105; ++id) {
        m.lessons.push_back({ .id = id, .participants = {{kC1A, kNoId}},
                             .subject_id = kMat, .teacher_id = kTA });
      }
      m.daily_load_rules = { {kNoId, kNoId, /*min=*/0, /*max=*/0, /*target=*/3,
        /*allowed_deviation=*/0, /*deviation_weight=*/10,
        /*imbalance_weight=*/40, /*overload_weight=*/20,
        /*underload_weight=*/30} };
      ScheduleSnapshot s{ {{100, {1, 0, kR1}},
                          {101, {1, 1, kR1}},
                          {102, {1, 2, kR1}},
                          {103, {1, 3, kR1}},
                          {104, {2, 0, kR1}}} };

      ScoreReport r = ComputeScore(m, s);
      const EntityScore& c1a = ByEntity(r.division_scores, kC1A);
      const PenaltyItem* dev = FindItem(c1a.items, "daily_deviation");
      ASSERT_NE(dev, nullptr);
      EXPECT_EQ(dev->penalty, 3 * 10 * 3);  // 3 periods off-target, prio 300%
      EXPECT_EQ(dev->count, 3u);
      const PenaltyItem* over = FindItem(c1a.items, "daily_overload");
      ASSERT_NE(over, nullptr);
      EXPECT_EQ(over->penalty, 1 * 20 * 3);
      const PenaltyItem* under = FindItem(c1a.items, "daily_underload");
      ASSERT_NE(under, nullptr);
      EXPECT_EQ(under->penalty, 2 * 30 * 3);
      const PenaltyItem* imb = FindItem(c1a.items, "daily_imbalance");
      ASSERT_NE(imb, nullptr);
      EXPECT_EQ(imb->penalty, 3 * 40 * 3);  // loads 4 vs 1
    }

    // A girl in the open group attends girls' PE, so her stream has no gap; the
    // boys' stream sees the hole. Exactly one gap must be counted — the fixed
    // split makes gap detection student-accurate.
    TEST(Scorer, FixedSplitLessonFillsGapForItsStudentsOnly) {
      SchoolModel m;
      m.days = { {kDay, "Pon", 4} };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {kY1, "Rok 1", 1, 100} };
      m.divisions = { {kC1A, "1A", kY1} };
      m.teachers = { {kTA, "TA"}, {kTB, "TB"} };
      m.subjects = { {kMat, "Matematyka"} };
      m.splits = { {.id = 5, .name = "lang", .division_id = kC1A,
                   .kind = SplitKind::kOpen},
                  {.id = 7, .name = "pe", .division_id = kC1A,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 30, .name = "o1", .division_id = kC1A, .split_id = 5},
                  {.id = 33, .name = "girls", .division_id = kC1A, .split_id = 7},
                  {.id = 34, .name = "boys", .division_id = kC1A, .split_id = 7} };
      m.lessons = { {.id = 100, .participants = {{kC1A, 30}}, .subject_id = kMat,
                    .teacher_id = kTA, .requires_room = false},
                   {.id = 101, .participants = {{kC1A, 30}}, .subject_id = kMat,
                    .teacher_id = kTA, .requires_room = false},
                   {.id = 102, .participants = {{kC1A, 33}}, .subject_id = kMat,
                    .teacher_id = kTB, .requires_room = false} };
      ScheduleSnapshot s{ {{100, {kDay, 0, kNoId}},
                          {101, {kDay, 2, kNoId}},
                          {102, {kDay, 1, kNoId}}} };  // girls fill the o1 hole

      ScoreReport r = ComputeScore(m, s);
      const PenaltyItem* gap = FindItem(r.global_items, "student_gap");
      ASSERT_NE(gap, nullptr);
      EXPECT_EQ(gap->count, 1u);  // only the o1-boys stream has the gap
      EXPECT_EQ(gap->penalty, Weights{}.student_gap_base);  // priority 100%
    }

    // A day with exactly one lesson instance is a comfort metric, not a search
    // penalty: it must appear as a located zero-penalty issue and never move
    // the objective or the penalty breakdown.
    TEST(Scorer, SingleLessonDayDetectedWithoutPenalty) {
      SchoolModel m;
      m.days = { {1, "Pon", 6}, {2, "Wt", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {kY1, "Rok 1", 1, 100} };
      m.divisions = { {kC1A, "1A", kY1} };
      m.teachers = { {kTA, "TA"} };
      m.subjects = { {kMat, "Matematyka"} };
      for (Id id = 100; id < 104; ++id) {
        m.lessons.push_back({ .id = id, .participants = {{kC1A, kNoId}},
                             .subject_id = kMat, .teacher_id = kTA,
                             .requires_room = false });
      }
      // Monday: one lonely lesson; Tuesday: three.
      ScheduleSnapshot s{ {{100, {1, 0, kNoId}},
                          {101, {2, 0, kNoId}},
                          {102, {2, 1, kNoId}},
                          {103, {2, 2, kNoId}}} };
      ScoreReport r = ComputeScore(m, s);
      int found = 0;
      for (const SoftIssue& issue : r.soft_issues) {
        if (issue.category != "single_lesson_day") continue;
        ++found;
        EXPECT_EQ(issue.penalty, 0);
        EXPECT_EQ(issue.day_id, 1u);  // Monday
        EXPECT_TRUE(issue.teacher);
      }
      EXPECT_EQ(found, 1);
      EXPECT_EQ(FindItem(r.global_items, "single_lesson_day"), nullptr);
    }

    // Rule modes at the scorer: hard = zero penalty but a config-hard issue;
    // off = neither, while the metric keeps observing the counts.
    TEST(Scorer, RuleModesChangeScoringNotObservation) {
      SchoolModel m = BaseModel();
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 4;  // 1A: two same-subject runs + gaps

      // subject_once HARD: the maths double-run is an ERROR, not a penalty.
      m.rule_config.overrides = { {"subject_once", RuleMode::kHard} };
      ScoreReport hard = ComputeScore(m, s);
      EXPECT_EQ(FindItem(ByEntity(hard.division_scores, kC1A).items,
        "subject_split")->penalty, 0);
      bool config_hard_issue = false;
      for (const SoftIssue& issue : hard.soft_issues) {
        config_hard_issue |= issue.category == "subject_split" &&
          issue.config_hard && issue.count > 0;
      }
      EXPECT_TRUE(config_hard_issue);
      // The repeats metric still observes the violation.
      const auto& repeats = *std::find_if(
        hard.metric_scores.begin(), hard.metric_scores.end(),
        [] (const MetricScore& ms) { return ms.key == "repeats"; });
      EXPECT_GT(repeats.count, 0);

      // subject_once OFF: no penalty, no issue, metric still counts.
      m.rule_config.overrides = { {"subject_once", RuleMode::kOff} };
      ScoreReport off = ComputeScore(m, s);
      for (const SoftIssue& issue : off.soft_issues) {
        EXPECT_NE(issue.category, "subject_split");
      }
      const auto& off_repeats = *std::find_if(
        off.metric_scores.begin(), off.metric_scores.end(),
        [] (const MetricScore& ms) { return ms.key == "repeats"; });
      EXPECT_EQ(off_repeats.count, repeats.count);
    }

    // Per-teacher overrides are Dobry Plan's comfort flags as data: a heavier
    // gap weight for one teacher, everything off for another.
    TEST(Scorer, TeacherScopedOverrides) {
      SchoolModel m = BaseModel();
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 4;  // TA gains 2 gaps; TB clean
      s.lessons[5].placement.start_period = 4;  // TB gains 2 gaps too

      m.rule_config.overrides = {
          {"teacher_gaps", RuleMode::kSoft, /*weight=*/450, 0, kNoId, kNoId,
           kNoId, /*teacher=*/kTA},
          {"teacher_gaps", RuleMode::kOff, 0, 0, kNoId, kNoId, kNoId,
          /*teacher=*/kTB} };
      ScoreReport r = ComputeScore(m, s);
      EXPECT_EQ(FindItem(ByEntity(r.teacher_scores, kTA).items, "teacher_gap")
        ->penalty,
        2 * 450);  // uczulony na okienka: 10x weight
      const PenaltyItem* tb = FindItem(ByEntity(r.teacher_scores, kTB).items,
        "teacher_gap");
      ASSERT_NE(tb, nullptr);   // elastyczny: counted for metrics...
      EXPECT_EQ(tb->penalty, 0);  // ...but never priced
    }

    TEST(Scorer, DailyLoadWithoutRuleAddsNothing) {
      ScoreReport r = ComputeScore(BaseModel(), CompactSnapshot());
      EXPECT_EQ(FindItem(r.global_items, "daily_deviation"), nullptr);
      EXPECT_EQ(FindItem(r.global_items, "daily_imbalance"), nullptr);
    }

    // The quality numbers are absolute: a clean schedule reads exactly 100, and
    // metric_scores expose the rates the composites were built from.
    TEST(Scorer, MetricScoresPopulatedAndQualityAbsolute) {
      ScoreReport clean = ComputeScore(BaseModel(), CompactSnapshot());
      ASSERT_FALSE(clean.metric_scores.empty());
      EXPECT_DOUBLE_EQ(clean.all_students_quality, 100.0);
      EXPECT_DOUBLE_EQ(clean.all_teachers_quality, 100.0);
      EXPECT_DOUBLE_EQ(clean.overall_quality, 100.0);
      for (const EntityScore& e : clean.division_scores) {
        EXPECT_DOUBLE_EQ(e.quality, 100.0) << e.name;
      }

      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 4;  // 1A: p0,p1,p4 -> 2 gaps
      ScoreReport gapped = ComputeScore(BaseModel(), s);
      EXPECT_LT(gapped.all_students_quality, 100.0);
      // BaseModel: 2 group-less divisions x 1 day = 2 stream-days; 2 gaps.
      const auto& g = *std::find_if(
        gapped.metric_scores.begin(), gapped.metric_scores.end(),
        [] (const MetricScore& m) { return m.key == "gaps"; });
      EXPECT_NEAR(g.rate, 1.0, 1e-9);
      EXPECT_EQ(g.count, 2);
      // The raw search objective is still reported, unchanged.
      EXPECT_GT(gapped.total_penalty, 0);
    }

    TEST(Scorer, PrebuiltContextScoresIdentically) {
      SchoolModel m = BaseModel();
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 4;  // some nonzero penalty
      ScoringContext context = ScoringContext::Build(m);
      ScoreReport from_context = ComputeScore(context, s);
      ScoreReport from_model = ComputeScore(m, s);
      EXPECT_EQ(from_context.total_penalty, from_model.total_penalty);
      EXPECT_EQ(from_context.overall_quality, from_model.overall_quality);
      EXPECT_EQ(from_context.soft_issues.size(), from_model.soft_issues.size());
    }

    TEST(Scorer, GapsDegradeQualityWithExactPenalty) {
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 4;  // 1A: p0,p1,p4 -> 2 gaps
      ScoreReport gapped = ComputeScore(BaseModel(), s);
      ScoreReport clean = ComputeScore(BaseModel(), CompactSnapshot());
      EXPECT_LT(gapped.overall_quality, clean.overall_quality);

      const EntityScore& c1a = ByEntity(gapped.division_scores, kC1A);
      const PenaltyItem* gap = FindItem(c1a.items, "student_gap");
      ASSERT_NE(gap, nullptr);
      EXPECT_EQ(gap->penalty, 2 * Weights{}.student_gap_base * 300 / 100);
      EXPECT_EQ(gap->count, 2u);

      // Teacher gap exists but costs less than the student gap.
      const EntityScore& ta = ByEntity(gapped.teacher_scores, kTA);
      const PenaltyItem* tgap = FindItem(ta.items, "teacher_gap");
      ASSERT_NE(tgap, nullptr);
      EXPECT_EQ(tgap->penalty, 2 * 45);
      EXPECT_LT(tgap->penalty, gap->penalty);
    }

    TEST(Scorer, StudentGapsAreUncapped) {
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 9;  // 1A: p0,p1,p9 -> 7 raw gaps
      ScoreReport r = ComputeScore(BaseModel(), s);
      const PenaltyItem* gap =
        FindItem(ByEntity(r.division_scores, kC1A).items, "student_gap");
      ASSERT_NE(gap, nullptr);
      EXPECT_EQ(gap->count, 7u);  // near-hard: every gap counts
      EXPECT_EQ(gap->penalty, 7 * Weights{}.student_gap_base * 300 / 100);

      // Teacher gaps in the same layout stay capped at gap_cap_per_day.
      const PenaltyItem* tgap =
        FindItem(ByEntity(r.teacher_scores, kTA).items, "teacher_gap");
      ASSERT_NE(tgap, nullptr);
      EXPECT_EQ(tgap->count, 3u);
      EXPECT_EQ(tgap->penalty, 3 * 45);
    }

    TEST(Scorer, LateLessonsDegradeQuality) {
      auto s = CompactSnapshot();
      // Move the whole 1A day to p6..p8: occupied p7 (cost 40) + p8 (cost 80).
      for (int i = 0; i < 3; ++i) {
        s.lessons[i].placement.start_period = 6 + i;
      }
      ScoreReport r = ComputeScore(BaseModel(), s);
      const PenaltyItem* late =
        FindItem(ByEntity(r.division_scores, kC1A).items, "late_student");
      ASSERT_NE(late, nullptr);
      EXPECT_EQ(late->penalty, (40 + 80) * 300 / 100);  // 360
      EXPECT_LT(r.overall_quality,
        ComputeScore(BaseModel(), CompactSnapshot()).overall_quality);
    }

    // Year priority scales SEARCH pressure (penalty) and the school composite's
    // weighting — but the absolute quality of an identical schedule shape is
    // identical by design (100 = pristine for everyone, no priority distortion).
    TEST(Scorer, YearPriorityScalesStudentPenalties) {
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 4;  // 1A gap x2
      s.lessons[5].placement.start_period = 4;  // 3A gap x2, identical shape
      ScoreReport r = ComputeScore(BaseModel(), s);
      const EntityScore& c1a = ByEntity(r.division_scores, kC1A);
      const EntityScore& c3a = ByEntity(r.division_scores, kC3A);
      EXPECT_EQ(c1a.penalty, 3 * c3a.penalty);  // 300 vs 100 priority
      EXPECT_DOUBLE_EQ(c1a.quality, c3a.quality);

      // The weighting shows up in the aggregate: fix ONLY the high-priority
      // division and the school score must improve more than fixing only the
      // low-priority one.
      auto fix_1a = s, fix_3a = s;
      fix_1a.lessons[2].placement.start_period = 2;
      fix_3a.lessons[5].placement.start_period = 2;
      EXPECT_GT(ComputeScore(BaseModel(), fix_1a).all_students_quality,
        ComputeScore(BaseModel(), fix_3a).all_students_quality);
    }

    TEST(Scorer, StabilityAndRoomChangePenalties) {
      SchoolModel m = BaseModel();
      m.lessons[0].previous_placement = Placement{ kDay, 5, kR1 };  // will differ
      auto s = CompactSnapshot();
      s.lessons[1].placement.room_id = kR2;  // TA now uses R1 and R2
      s.lessons[4].placement.room_id = kR3;  // keep 3A rooms conflict-free
      ScoreReport r = ComputeScore(m, s);
      EXPECT_NE(FindItem(ByEntity(r.division_scores, kC1A).items, "stability"),
        nullptr);
      const PenaltyItem* rc =
        FindItem(ByEntity(r.teacher_scores, kTA).items, "room_change");
      ASSERT_NE(rc, nullptr);
      EXPECT_EQ(rc->penalty, 10);
    }

    TEST(Scorer, SubjectSplitOnSameDayNonAdjacent) {
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 5;  // Mat pairs (p0,p1,p5): one split
      ScoreReport r = ComputeScore(BaseModel(), s);
      const PenaltyItem* split =
        FindItem(ByEntity(r.division_scores, kC1A).items, "subject_split");
      ASSERT_NE(split, nullptr);
      // Runs: [p0,p1] and [p5] -> 2 runs -> 1 split x 70 x priority 3.
      EXPECT_EQ(split->penalty, 1 * 70 * 3);
    }

    TEST(Scorer, SoftIssuesAreLocated) {
      auto s = CompactSnapshot();
      s.lessons[2].placement.start_period = 4;  // 1A: p0,p1,p4 -> gaps p2,p3
      ScoreReport r = ComputeScore(BaseModel(), s);
      const SoftIssue* gap = nullptr;
      for (const auto& issue : r.soft_issues) {
        if (issue.category == "student_gap" && !issue.teacher) gap = &issue;
      }
      ASSERT_NE(gap, nullptr);
      EXPECT_EQ(gap->entity, "1A");
      EXPECT_EQ(gap->entity_id, kC1A);
      EXPECT_EQ(gap->day_id, kDay);
      EXPECT_EQ(gap->period, 2u);  // first empty period
      EXPECT_EQ(gap->count, 2u);
      EXPECT_GT(gap->penalty, 0);
      // Sorted by penalty: first issue carries the largest penalty.
      EXPECT_GE(r.soft_issues.front().penalty, r.soft_issues.back().penalty);
    }

    // Mixed partitions (the real "1t" case): when BOTH halves of the 2-way
    // Mixed partitions (the real "1t" case). Atom streams make overlap exact:
    // the "3" partition here has a single declared group (3/3), so it adds no
    // dimension — the division has 2 atoms, (1/2·3/3) and (2/2·3/3). When both
    // halves of the 2-way partition run at a slot, every atom is busy, so no
    // phantom gap. When only one half runs, exactly the OTHER atom sees a gap.
    TEST(Scorer, StudentGapIsCountedPerGroup) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "Rok 1", 1, 300} };
      m.divisions = { {20, "1t", 10} };
      m.groups = { {31, "1/2", 20}, {32, "2/2", 20} };
      m.teachers = { {40, "TA"}, {41, "TB"}, {42, "TC"} };
      m.subjects = { {50, "j.polski"}, {51, "adm.sys.op"} };
      m.rooms = { {70, "R1", "R1"}, {71, "R2", "R2"}, {72, "R3", "R3"} };
      m.lessons = {
          {.id = 100, .participants = {{20, kNoId}}, .subject_id = 50,
           .teacher_id = 40},  // whole class
          {.id = 101, .participants = {{20, 31}}, .subject_id = 51,
           .teacher_id = 41},  // group 1/2
          {.id = 102, .participants = {{20, 32}}, .subject_id = 51,
           .teacher_id = 42},  // group 2/2
      };
      // Both group lessons run right after the whole-class lesson: each group is
      // occupied at p1 and p2 with no hole, so no student gap.
      ScheduleSnapshot tight{ {
          {100, {1, 1, 70}}, {101, {1, 2, 71}}, {102, {1, 2, 72}},
      } };
      ScoreReport r = ComputeScore(m, tight);
      EXPECT_EQ(FindItem(r.global_items, "student_gap"), nullptr)
        << "each group is contiguous at p1,p2";

      // Group 1/2's lesson moves to p3, leaving that group idle at p2 while the
      // whole class ran at p1: exactly one student gap, for group 1/2 only.
      ScheduleSnapshot gapped{ {
          {100, {1, 1, 70}}, {101, {1, 3, 71}}, {102, {1, 2, 72}},
      } };
      ScoreReport r2 = ComputeScore(m, gapped);
      const PenaltyItem* gap = FindItem(r2.global_items, "student_gap");
      ASSERT_NE(gap, nullptr);
      EXPECT_EQ(gap->count, 1u);  // group 1/2 gaps at p2; group 2/2 is contiguous
    }

    TEST(Scorer, BreakdownStructureIsComplete) {
      ScoreReport r = ComputeScore(BaseModel(), CompactSnapshot());
      EXPECT_EQ(r.division_scores.size(), 2u);
      EXPECT_EQ(r.year_scores.size(), 2u);
      EXPECT_EQ(r.teacher_scores.size(), 2u);
      EXPECT_GE(r.all_students_quality, 0.0);
      EXPECT_LE(r.all_students_quality, 100.0);
    }

  }  // namespace
}  // namespace arrango
