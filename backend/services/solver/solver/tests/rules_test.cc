// tests/rules_test.cc

#include <gtest/gtest.h>

#include <set>
#include <string>

#include "score/metrics.h"
#include "score/rules.h"

namespace arrango {
  namespace {

    // Minimal model with one of each scopable entity for resolver fixtures.
    SchoolModel M() {
      SchoolModel m;
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Chemia"}, {51, "Mat"} };
      return m;
    }

    RuleResolver Build(const SchoolModel& m, const RuleConfig& request = {}) {
      return RuleResolver::Build(m, ModelIndex::Build(m), request);
    }

    TEST(Rules, TableIntegrity) {
      std::set<std::string> keys;
      std::set<std::string> metric_keys;
      for (const MetricDef& d : MetricTable()) metric_keys.insert(d.key);
      for (const RuleDef& def : RuleTable()) {
        EXPECT_TRUE(keys.insert(def.key).second) << "duplicate " << def.key;
        EXPECT_FALSE(def.categories.empty()) << def.key;
        if (def.metric[0] != '\0') {
          EXPECT_TRUE(metric_keys.count(def.metric))
            << def.key << " -> unknown metric " << def.metric;
        }
        EXPECT_NE(def.default_mode, RuleMode::kDefault) << def.key;
      }
      EXPECT_TRUE(keys.count("subject_once"));
      EXPECT_TRUE(keys.count("gap_windows"));
    }

    TEST(Rules, DefaultsReproduceToday) {
      RuleResolver r = Build(M());
      EXPECT_EQ(r.SchoolWide("student_gaps").mode, RuleMode::kSoft);
      EXPECT_EQ(r.SchoolWide("gap_windows").mode, RuleMode::kHard);
      EXPECT_EQ(r.SchoolWide("subject_once").mode, RuleMode::kSoft);
      EXPECT_EQ(r.SchoolWide("anti_split_shift").mode, RuleMode::kOff);
      // 0 = "use the built-in weight formula".
      EXPECT_EQ(r.SchoolWide("student_gaps").weight, 0);
      EXPECT_TRUE(r.Diagnostics().empty());
    }

    TEST(Rules, LayerPrecedenceLastMatchWins) {
      SchoolModel m = M();
      m.rule_config.profile = "dobry_plan";  // subject_once -> hard
      // Model: soften subject_once for chemistry (podwojna dawka).
      m.rule_config.overrides = {
          {"subject_once", RuleMode::kOff, 0, 0, kNoId, kNoId, /*subject=*/50} };
      // Request: harden it back for chemistry — request layer wins.
      RuleConfig request;
      request.overrides = {
          {"subject_once", RuleMode::kHard, 0, 0, kNoId, kNoId, /*subject=*/50} };

      RuleResolver model_only = Build(m);
      EXPECT_EQ(model_only.For("subject_once", 10, 20, 50, kNoId).mode,
        RuleMode::kOff);   // chemistry exempt
      EXPECT_EQ(model_only.For("subject_once", 10, 20, 51, kNoId).mode,
        RuleMode::kHard);  // maths keeps the profile's hard

      RuleResolver with_request = Build(m, request);
      EXPECT_EQ(with_request.For("subject_once", 10, 20, 50, kNoId).mode,
        RuleMode::kHard);  // request layered last
      // SchoolWide ignores subject-scoped layers entirely.
      EXPECT_EQ(with_request.SchoolWide("subject_once").mode, RuleMode::kHard);
    }

    TEST(Rules, WithinListLastMatchWins) {
      SchoolModel m = M();
      m.rule_config.overrides = {
          {"teacher_gaps", RuleMode::kOff, 0, 0, kNoId, kNoId, kNoId, /*t=*/40},
          {"teacher_gaps", RuleMode::kSoft, /*weight=*/180, 0, kNoId, kNoId,
           kNoId, /*t=*/40} };
      ResolvedRule resolved = Build(m).For("teacher_gaps", kNoId, kNoId, kNoId, 40);
      EXPECT_EQ(resolved.mode, RuleMode::kSoft);
      EXPECT_EQ(resolved.weight, 180);
    }

    TEST(Rules, DiagnosticsNameEveryConfigMistake) {
      SchoolModel m = M();
      m.rule_config.profile = "very_strict";  // unknown profile
      m.rule_config.overrides = {
          {"no_such_rule", RuleMode::kOff},
          {"stability", RuleMode::kHard},  // not hardable
          {"teacher_gaps", RuleMode::kOff, 0, 0, /*year=*/10},  // wrong scope
          {"subject_once", RuleMode::kOff, 0, 0, kNoId, kNoId, /*subject=*/999} };
      // Five mistakes: profile, rule name, non-hardable, wrong scope, bad id.
      RuleResolver resolver = Build(m);  // keep alive: Diagnostics() borrows
      const std::vector<std::string>& d = resolver.Diagnostics();
      ASSERT_EQ(d.size(), 5u);
      auto has = [&] (const char* needle) {
        for (const std::string& msg : d) {
          if (msg.find(needle) != std::string::npos) return true;
        }
        return false;
        };
      EXPECT_TRUE(has("very_strict"));
      EXPECT_TRUE(has("no_such_rule"));
      EXPECT_TRUE(has("cannot be hard"));
      EXPECT_TRUE(has("unknown subject id 999"));
      EXPECT_TRUE(has("does not take year"));
    }

  }  // namespace
}  // namespace arrango
