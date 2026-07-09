// tests/daily_load_test.cc

#include <gtest/gtest.h>

#include "model/atoms.h"
#include "model/daily_load.h"
#include "model/index.h"

namespace arrango {
  namespace {

    // One division, no groups -> one atom. `weekly` single-period lessons over
    // `days` five-period days.
    SchoolModel LoadModel(int days, int weekly) {
      SchoolModel m;
      for (int d = 0; d < days; ++d) {
        m.days.push_back({ static_cast<Id>(1 + d), "D", 5 });
      }
      for (Id p = 1; p <= 5; ++p) m.periods.push_back({ static_cast<Id>(200 + p), "" });
      m.divisions = { {.id = 20, .name = "1A"} };
      m.subjects = { {.id = 50, .name = "x"} };
      for (int i = 0; i < weekly; ++i) {
        m.lessons.push_back({ .id = static_cast<Id>(100 + i),
                             .participants = {{20, kNoId}},
                             .subject_id = 50,
                             .requires_teacher = false,
                             .requires_room = false });
      }
      return m;
    }

    EffectiveDailyLoad Resolve(const SchoolModel& m) {
      ModelIndex ix = ModelIndex::Build(m);
      AtomSet atoms = BuildAtoms(m, ix);
      return ResolveDailyLoad(m, ix, atoms, /*division_idx=*/0);
    }

    TEST(DailyLoad, BuiltInDefaultMinIsThree) {
      // 30 weekly over 5 days: floor(30/5)=6 >= 3, so min stays 3.
      EXPECT_EQ(Resolve(LoadModel(5, 30)).min_per_day, 3u);
    }

    TEST(DailyLoad, MinAutoRelaxesToWeeklyOverDays) {
      // 10 weekly over 5 days -> floor(10/5)=2 -> min relaxes to 2.
      EXPECT_EQ(Resolve(LoadModel(5, 10)).min_per_day, 2u);
      // 4 weekly over 5 days -> floor(4/5)=0 -> min relaxes to 0.
      EXPECT_EQ(Resolve(LoadModel(5, 4)).min_per_day, 0u);
    }

    TEST(DailyLoad, TargetAutoIsCeilWeeklyOverDays) {
      // 28 weekly over 5 days -> ceil(28/5)=6.
      EXPECT_EQ(Resolve(LoadModel(5, 28)).target_per_day, 6u);
    }

    TEST(DailyLoad, DivisionRuleBeatsDefaultAndGroupRulesIgnored) {
      SchoolModel m = LoadModel(5, 30);
      m.groups = { {.id = 30, .name = "g", .division_id = 20} };
      // Group-scoped rules never apply to the division min/max; a division rule
      // beats the school default.
      m.daily_load_rules = {
          {.division_id = kNoId, .max_per_day = 10},              // default
          {.division_id = 20, .group_id = 30, .max_per_day = 4},  // group: IGNORED
          {.division_id = 20, .max_per_day = 8},                  // division
      };
      EXPECT_EQ(Resolve(m).max_per_day, 8u);  // division rule, not the group's 4

      m.daily_load_rules.pop_back();  // drop division rule -> default wins
      EXPECT_EQ(Resolve(m).max_per_day, 10u);
    }

    TEST(DailyLoad, DivisionDailyLoadsUnionCountAndDurations) {
      SchoolModel m = LoadModel(5, 0);
      // Two parallel group lessons at the same period count as ONE division
      // period; a 2-period block counts as 2.
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20},
                  {.id = 31, .name = "2/2", .division_id = 20} };
      m.lessons = {
          {.id = 100, .participants = {{20, 30}}, .subject_id = 50,
           .requires_teacher = false, .requires_room = false},
          {.id = 101, .participants = {{20, 31}}, .subject_id = 50,
           .requires_teacher = false, .requires_room = false},
          {.id = 102, .participants = {{20, kNoId}}, .subject_id = 50,
           .duration = 2, .requires_teacher = false, .requires_room = false},
      };
      ModelIndex ix = ModelIndex::Build(m);
      ScheduleSnapshot s{ {
          {100, {1, 0, kNoId}},  // 1/2 at day0 p0
          {101, {1, 0, kNoId}},  // 2/2 parallel at day0 p0 -> still 1 period
          {102, {1, 1, kNoId}},  // whole-class block day0 p1-2 -> 2 periods
      } };
      auto loads = DivisionDailyLoads(m, ix, s);
      ASSERT_EQ(loads.size(), 1u);
      EXPECT_EQ(loads[0][0], 3u);  // period 0 (union) + periods 1,2 = 3
      EXPECT_EQ(loads[0][1], 0u);
    }

  }  // namespace
}  // namespace arrango
