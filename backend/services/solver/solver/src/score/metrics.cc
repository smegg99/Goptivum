// src/score/metrics.cc

#include "score/metrics.h"

#include <cmath>

namespace arrango {
  namespace {

    // Config override, or the table default when the key is absent.
    double Resolve(const std::map<std::string, double>& overrides,
      const std::string& key, double fallback) {
      auto it = overrides.find(key);
      return it != overrides.end() && it->second > 0 ? it->second : fallback;
    }

  }  // namespace

  const std::vector<MetricDef>& MetricTable() {
    // Defaults confirmed by user survey 2026-07-08: pristine 100, a decent
    // real schedule ~77, a barely-polished construct ~25.
    static const std::vector<MetricDef> table = {
      { "gaps",        false, 0.10, 2.0 },  // gap periods / stream-day
      { "gap_windows", false, 0.02, 3.0 },  // adjacent-gap pairs / stream-day
      { "lateness",    false, 0.20, 1.0 },  // share of periods past threshold
      { "repeats",     false, 0.08, 1.0 },  // extra same-subject runs / div-day
      { "load_band",   false, 0.15, 1.0 },  // out-of-band periods / div-day
      { "overload",    false, 0.10, 1.0 },  // over max-lessons prefs / div-day
      { "t_gaps",      true,  0.50, 2.0 },  // counted gaps / teacher-day
      { "room_moves",  true,  1.50, 1.0 },  // room switches / teacher-day
      { "t_late",      true,  0.30, 1.0 },  // share of periods past threshold
      { "single_days", true,  0.15, 1.0 },  // share of active teacher-days
    };
    return table;
  }

  std::vector<MetricScore> ComputeMetricScores(const MetricInputs& inputs,
    const RatingConfig& config) {
    std::vector<MetricScore> scores;
    for (const MetricDef& def : MetricTable()) {
      auto it = inputs.by_key.find(def.key);
      if (it == inputs.by_key.end()) continue;  // not measured -> not scored
      const MetricInputs::Entry& in = it->second;
      MetricScore s;
      s.key = def.key;
      s.teachers = def.teachers;
      s.count = in.count;
      s.applicable = in.applicable && in.denominator > 0;
      if (s.applicable) {
        s.rate = static_cast<double>(in.count) /
          static_cast<double>(in.denominator);
        const double hl = Resolve(config.half_life, def.key, def.half_life);
        s.subscore = 100.0 * std::pow(2.0, -s.rate / hl);
      }
      scores.push_back(std::move(s));
    }
    return scores;
  }

  double CompositeScore(const std::vector<MetricScore>& scores, bool teachers,
    const RatingConfig& config) {
    double sum = 0.0, weight_sum = 0.0;
    for (const MetricDef& def : MetricTable()) {
      if (def.teachers != teachers) continue;
      for (const MetricScore& s : scores) {
        if (s.key != def.key || !s.applicable) continue;
        const double w = Resolve(config.weight, def.key, def.weight);
        sum += s.subscore * w;
        weight_sum += w;
      }
    }
    return weight_sum > 0 ? sum / weight_sum : 100.0;
  }

  double OverallScore(double students, double teachers,
    const RatingConfig& config) {
    const double share =
      config.students_share > 0 ? config.students_share : 0.70;
    return share * students + (1.0 - share) * teachers;
  }

}  // namespace arrango
