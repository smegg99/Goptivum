// src/score/scorer_aggregate.cc

#include <algorithm>
#include <map>
#include <vector>

#include "score/penalty_defs.h"
#include "score/scorer_internal.h"

namespace arrango {
namespace scoring {

namespace {

// Count of a category in a CatMap, 0 when absent.
int64_t CountOf(const CatMap& cats, const char* category) {
  auto it = cats.find(category);
  return it == cats.end() ? 0 : static_cast<int64_t>(it->second.count);
}

}  // namespace

// Per-division rating inputs (rates defined in score/metrics.h). The
// denominators: stream-days for gap metrics, active days for per-day
// metrics, stream-member periods for the lateness share.
MetricInputs Scorer::DivisionMetricInputs(size_t c, int64_t active_days,
                                          bool load_rules,
                                          bool max_prefs) const {
  const CatMap& cats = division_cats_[c];
  const int64_t stream_days =
      static_cast<int64_t>(atoms_.of_division[c].size()) * active_days;
  MetricInputs in;
  in.by_key["gaps"] = { CountOf(cats, kCatStudentGap), stream_days, true };
  in.by_key["gap_windows"] = { CountOf(cats, kCatGapWindow), stream_days,
                              true };
  in.by_key["lateness"] = { late_student_periods_of_division_[c],
                           student_periods_of_division_[c], true };
  in.by_key["repeats"] = { CountOf(cats, kCatSubjectSplit) +
                              CountOf(cats, kCatBlockBreak),
                          active_days, true };
  in.by_key["load_band"] = { CountOf(cats, kCatDailyDeviation) +
                                CountOf(cats, kCatDailyOverload) +
                                CountOf(cats, kCatDailyUnderload),
                            active_days, load_rules };
  in.by_key["overload"] = { CountOf(cats, kCatMaxLessons), active_days,
                           max_prefs };
  return in;
}

MetricInputs Scorer::TeacherMetricInputs(size_t t,
                                         int64_t active_days) const {
  const CatMap& cats = teacher_cats_[t];
  MetricInputs in;
  in.by_key["t_gaps"] = { CountOf(cats, kCatTeacherGap), active_days, true };
  in.by_key["room_moves"] = { CountOf(cats, kCatRoomChange), active_days,
                             true };
  in.by_key["t_late"] = { late_periods_of_teacher_[t],
                         periods_of_teacher_[t], true };
  in.by_key["single_days"] = { single_days_of_teacher_[t],
                              active_days_of_teacher_[t], true };
  return in;
}

ScoreReport Scorer::Aggregate() {
  ScoreReport report;
  std::sort(issues_.begin(), issues_.end(),
            [] (const SoftIssue& a, const SoftIssue& b) {
              return a.penalty > b.penalty;
            });
  report.soft_issues = std::move(issues_);
  report.info_issues = std::move(info_issues_);

  int64_t active_days = 0;
  for (const Day& d : m_.days) {
    if (d.period_count >= 1) ++active_days;
  }
  const bool load_rules = !m_.daily_load_rules.empty();
  bool max_prefs = false;
  for (const Preference& p : m_.preferences) {
    max_prefs |= p.kind == PreferenceKind::kMaxLessonsPerDay;
  }

  std::vector<int64_t> division_lessons(m_.divisions.size(), 0);
  for (size_t c = 0; c < m_.divisions.size(); ++c) {
    division_lessons[c] = static_cast<int64_t>(
        ix_.LessonsOfDivision(static_cast<int>(c)).size());
  }

  // School-level inputs = sum of entity inputs (same keys, same units).
  MetricInputs school;
  auto accumulate = [&school] (const MetricInputs& in) {
    for (const auto& [key, e] : in.by_key) {
      MetricInputs::Entry& acc = school.by_key[key];
      acc.count += e.count;
      acc.denominator += e.denominator;
      acc.applicable = e.applicable;  // school-wide flags, same everywhere
    }
    };

  struct YearAcc {
    int64_t penalty{};
    int64_t lessons{};
    double weighted_quality{};
  };
  std::map<int, YearAcc> per_year;
  double students_weighted = 0, students_weight = 0;

  for (size_t c = 0; c < m_.divisions.size(); ++c) {
    EntityScore score;
    score.entity_id = m_.divisions[c].id;
    score.name = m_.divisions[c].name;
    score.items = ToItems(division_cats_[c], &score.penalty);
    const MetricInputs inputs =
        DivisionMetricInputs(c, active_days, load_rules, max_prefs);
    accumulate(inputs);
    score.quality =
        CompositeScore(ComputeMetricScores(inputs, rating_), false, rating_);
    report.total_penalty += score.penalty;

    int y = ix_.YearIdx(m_.divisions[c].year_id);
    // Weighting matches the old aggregation: lesson load x year priority.
    double weight =
        static_cast<double>(division_lessons[c]) * ix_.YearPriority(y);
    if (y >= 0) {
      per_year[y].penalty += score.penalty;
      per_year[y].lessons += division_lessons[c];
      per_year[y].weighted_quality +=
          score.quality * static_cast<double>(division_lessons[c]);
    }
    students_weighted += score.quality * weight;
    students_weight += weight;
    report.division_scores.push_back(std::move(score));
  }

  for (const auto& [y, acc] : per_year) {
    EntityScore score;
    score.entity_id = m_.years[y].id;
    score.name = m_.years[y].name;
    score.penalty = acc.penalty;
    score.quality = acc.lessons > 0
                        ? acc.weighted_quality / static_cast<double>(acc.lessons)
                        : 100.0;
    report.year_scores.push_back(std::move(score));
  }

  double teachers_weighted = 0, teachers_weight = 0;
  for (size_t t = 0; t < m_.teachers.size(); ++t) {
    int64_t load = 0;
    for (int li : ix_.LessonsOfTeacher(static_cast<int>(t))) {
      load += m_.lessons[li].duration;
    }
    EntityScore score;
    score.entity_id = m_.teachers[t].id;
    score.name = m_.teachers[t].name;
    score.items = ToItems(teacher_cats_[t], &score.penalty);
    const MetricInputs inputs = TeacherMetricInputs(t, active_days);
    accumulate(inputs);
    score.quality =
        CompositeScore(ComputeMetricScores(inputs, rating_), true, rating_);
    report.total_penalty += score.penalty;
    teachers_weighted += score.quality * static_cast<double>(load);
    teachers_weight += static_cast<double>(load);
    report.teacher_scores.push_back(std::move(score));
  }

  report.metric_scores = ComputeMetricScores(school, rating_);
  report.all_students_quality =
      students_weight > 0 ? students_weighted / students_weight : 100.0;
  report.all_teachers_quality =
      teachers_weight > 0 ? teachers_weighted / teachers_weight : 100.0;
  report.overall_quality = OverallScore(report.all_students_quality,
                                        report.all_teachers_quality, rating_);

  CatMap global;
  for (const auto& cats : { division_cats_, teacher_cats_ }) {
    for (const CatMap& c : cats) {
      for (const auto& [cat, acc] : c) {
        global[cat].penalty += acc.penalty;
        global[cat].count += acc.count;
      }
    }
  }
  int64_t ignored = 0;
  report.global_items = ToItems(global, &ignored);
  return report;
}

}  // namespace scoring
}  // namespace arrango
