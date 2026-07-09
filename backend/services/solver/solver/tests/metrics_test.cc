// tests/metrics_test.cc

#include <gtest/gtest.h>

#include <algorithm>
#include <cmath>

#include "score/metrics.h"

namespace arrango {
  namespace {

    const MetricScore& ByKey(const std::vector<MetricScore>& v, const char* key) {
      auto it = std::find_if(v.begin(), v.end(),
        [key] (const MetricScore& s) { return s.key == key; });
      EXPECT_NE(it, v.end()) << key;
      return *it;
    }

    TEST(Metrics, PristineIsExactlyHundred) {
      MetricInputs in;
      for (const MetricDef& d : MetricTable()) in.by_key[d.key] = { 0, 100, true };
      auto scores = ComputeMetricScores(in, {});
      ASSERT_EQ(scores.size(), MetricTable().size());
      for (const auto& s : scores) EXPECT_DOUBLE_EQ(s.subscore, 100.0) << s.key;
      EXPECT_DOUBLE_EQ(CompositeScore(scores, /*teachers=*/false, {}), 100.0);
      EXPECT_DOUBLE_EQ(CompositeScore(scores, /*teachers=*/true, {}), 100.0);
    }

    TEST(Metrics, HalfLifeHalvesTheScore) {
      MetricInputs in;  // gaps rate == its default half-life (0.10): 55/550
      in.by_key["gaps"] = { 55, 550, true };
      auto scores = ComputeMetricScores(in, {});
      const MetricScore& g = ByKey(scores, "gaps");
      EXPECT_NEAR(g.rate, 0.10, 1e-9);
      EXPECT_NEAR(g.subscore, 50.0, 1e-9);
      EXPECT_EQ(g.count, 55);
      EXPECT_FALSE(g.teachers);
    }

    TEST(Metrics, OverridesAndInapplicable) {
      MetricInputs in;
      in.by_key["gaps"] = { 55, 550, true };
      in.by_key["load_band"] = { 999, 1, false };  // no rule -> excluded
      RatingConfig cfg;
      cfg.half_life["gaps"] = 0.20;  // softer curve: same rate scores ~70.7
      auto scores = ComputeMetricScores(in, cfg);
      const MetricScore& g = ByKey(scores, "gaps");
      EXPECT_NEAR(g.subscore, 100.0 * std::pow(2.0, -0.5), 1e-9);
      EXPECT_FALSE(ByKey(scores, "load_band").applicable);
      // Composite ignores inapplicable and absent metrics: only gaps counts.
      EXPECT_NEAR(CompositeScore(scores, false, cfg), g.subscore, 1e-9);
    }

    TEST(Metrics, ZeroDenominatorIsInapplicable) {
      MetricInputs in;
      in.by_key["t_gaps"] = { 5, 0, true };
      auto scores = ComputeMetricScores(in, {});
      EXPECT_FALSE(ByKey(scores, "t_gaps").applicable);
      EXPECT_DOUBLE_EQ(CompositeScore(scores, true, {}), 100.0);
    }

    TEST(Metrics, CompositeUsesWeights) {
      // Two teacher metrics, subscores 100 and 50, default weights 2 (t_gaps)
      // and 1 (room_moves): composite = (2*50 + 1*100) / 3.
      MetricInputs in;
      in.by_key["t_gaps"] = { 5, 10, true };      // rate 0.5 == hl -> 50
      in.by_key["room_moves"] = { 0, 10, true };  // 100
      auto scores = ComputeMetricScores(in, {});
      EXPECT_NEAR(CompositeScore(scores, true, {}), (2 * 50.0 + 100.0) / 3.0, 1e-9);
    }

    TEST(Metrics, OverallShare) {
      RatingConfig cfg;  // default 0.70
      EXPECT_NEAR(OverallScore(80.0, 60.0, cfg), 74.0, 1e-9);
      cfg.students_share = 0.5;
      EXPECT_NEAR(OverallScore(80.0, 60.0, cfg), 70.0, 1e-9);
    }

  }  // namespace
}  // namespace arrango
