// src/score/metrics.h

#pragma once

#include <cstdint>
#include <map>
#include <string>
#include <vector>

namespace arrango {

  // Natural-unit rating, fully decoupled from the CP-SAT search weights.
  // Each metric is a violation RATE (count / denominator, e.g. gaps per
  // stream-day) mapped to a subscore by
  //     subscore = 100 * 2^(-rate / half_life)
  // — it halves every half_life of rate and reads exactly 100 at rate 0,
  // so 100 == pristine on an absolute, school-size-independent scale.

  // One metric's identity and defaults. THE table (students first); keys
  // are stable API strings — ReportingWeights maps are keyed by them.
  struct MetricDef {
    const char* key;
    bool teachers;     // composite group
    double half_life;  // rate at which the subscore reads 50
    double weight;     // default composite weight
  };
  const std::vector<MetricDef>& MetricTable();

  // Reporting configuration (proto ReportingWeights). Absent key = default
  // from the table; students_share splits the overall composite.
  struct RatingConfig {
    std::map<std::string, double> half_life;
    std::map<std::string, double> weight;
    double students_share{ 0.70 };
  };

  // Raw counts + denominators, filled by the scorer. applicable == false
  // (no daily-load rule, no max-lessons pref, empty denominator) excludes
  // the metric from rates AND composites instead of scoring a fake 100.
  struct MetricInputs {
    struct Entry {
      int64_t count{};
      int64_t denominator{};
      bool applicable{ true };
    };
    std::map<std::string, Entry> by_key;
  };

  struct MetricScore {
    std::string key;
    bool teachers{};
    bool applicable{ true };
    double rate{};
    double subscore{};
    int64_t count{};
  };

  // One score per table metric PRESENT in the inputs (absent = skipped).
  std::vector<MetricScore> ComputeMetricScores(const MetricInputs& inputs,
    const RatingConfig& config);
  // Weighted mean over the group's applicable metrics; 100 when none apply.
  double CompositeScore(const std::vector<MetricScore>& scores, bool teachers,
    const RatingConfig& config);
  double OverallScore(double students, double teachers,
    const RatingConfig& config);

  // Hygiene (INFO) detector thresholds — reported, never scored.
  inline constexpr uint32_t kInfoStartVarianceMax = 2;   // periods
  inline constexpr uint32_t kInfoLatestFirstLesson = 2;  // 0-based period

}  // namespace arrango
