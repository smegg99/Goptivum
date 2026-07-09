// tests/demo_data_test.cc

#include <gtest/gtest.h>

#include <set>

#include "demo/demo_data.h"
#include "model/index.h"
#include "score/rules.h"
#include "solve/candidates.h"
#include "validate/preflight.h"

namespace arrango {
  namespace {

    TEST(DemoData, PresetsAreDeterministic) {
      EXPECT_EQ(GenerateDemoSchool(DemoPreset::kProduction, 7),
        GenerateDemoSchool(DemoPreset::kProduction, 7));
      EXPECT_EQ(GenerateDemoSchool(DemoPreset::kMega, 7),
        GenerateDemoSchool(DemoPreset::kMega, 7));
    }

    TEST(DemoData, ProductionMatchesTheRealExportShape) {
      SchoolModel m = GenerateDemoSchool(DemoPreset::kProduction, 1);
      EXPECT_EQ(m.divisions.size(), 34u);
      EXPECT_GE(m.teachers.size(), 80u);
      EXPECT_GE(m.rooms.size(), 38u);
      EXPECT_GT(m.lessons.size(), 1000u);
      EXPECT_TRUE(m.lesson_links.empty());  // raw problem only
    }

    // The mega preset is the feature-complete sibling: bigger than production
    // AND exercising every capability the solver has.
    TEST(DemoData, MegaContainsEveryCapability) {
      SchoolModel production = GenerateDemoSchool(DemoPreset::kProduction, 1);
      SchoolModel m = GenerateDemoSchool(DemoPreset::kMega, 1);
      EXPECT_GT(m.divisions.size(), production.divisions.size());
      EXPECT_GT(m.lessons.size(), production.lessons.size());
      EXPECT_GT(m.teachers.size(), production.teachers.size());
      EXPECT_GT(m.rooms.size(), production.rooms.size());

      // Splits of both kinds.
      bool open = false, fixed = false;
      for (const Split& s : m.splits) {
        open |= s.kind == SplitKind::kOpen;
        fixed |= s.kind == SplitKind::kFixed;
      }
      EXPECT_TRUE(open);
      EXPECT_TRUE(fixed);
      // Links of all three kinds.
      std::set<LessonLinkKind> kinds;
      for (const LessonLink& link : m.lesson_links) kinds.insert(link.kind);
      EXPECT_TRUE(kinds.count(LessonLinkKind::kSameDay));
      EXPECT_TRUE(kinds.count(LessonLinkKind::kDifferentDay));
      EXPECT_TRUE(kinds.count(LessonLinkKind::kConsecutive));
      // Edge lessons, locks, multi-period lessons, parallel blocks.
      int edges = 0, locks = 0, doubles = 0, parallel = 0;
      for (const LessonInstance& l : m.lessons) {
        edges += l.edge != EdgePlacement::kNone;
        locks += l.locked;
        doubles += l.duration > 1;
        parallel += l.parallel_block_id != kNoId;
      }
      EXPECT_GE(edges, 4);
      EXPECT_GE(locks, 2);
      EXPECT_GT(doubles, 0);
      EXPECT_GT(parallel, 0);
      // Rules, load bands, preferences, blocks of several targets.
      EXPECT_GE(m.rule_config.overrides.size(), 5u);
      EXPECT_FALSE(m.daily_load_rules.empty());
      EXPECT_GE(m.preferences.size(), 2u);
      std::set<BlockTarget> targets;
      for (const ExternalBlock& b : m.external_blocks) targets.insert(b.target);
      EXPECT_GE(targets.size(), 3u);
    }

    // Both presets must be VALID INPUTS: rule config resolves clean, the
    // preflight finds no hard impossibility, and candidates materialize.
    TEST(DemoData, PresetsPassPreflightAndBuildCandidates) {
      for (DemoPreset preset : {DemoPreset::kProduction, DemoPreset::kMega}) {
        SchoolModel m = GenerateDemoSchool(preset, 1);
        ModelIndex ix = ModelIndex::Build(m);
        RuleResolver rules = RuleResolver::Build(m, ix, {});
        EXPECT_TRUE(rules.Diagnostics().empty());
        PreflightReport report = RunPreflight(m, ix, 0, rules);
        for (const Conflict& c : report.hard) {
          ADD_FAILURE() << "preset hard finding: " << c.message;
        }
        auto built = BuildCandidates(m, ix, UnknownCapacityPolicy::kAllow, 0,
          &rules);
        EXPECT_TRUE(std::holds_alternative<CandidateSet>(built))
          << (std::holds_alternative<std::string>(built)
            ? std::get<std::string>(built)
            : "");
      }
    }

  }  // namespace
}  // namespace arrango
