// tests/explain_test.cc

#include <gtest/gtest.h>

#include <algorithm>

#include "solve/explain.h"
#include "solve/solver.h"

namespace arrango {
  namespace {

    // One 6-period day, one division, two teachers, roomless — small enough to
    // force contradictions deliberately.
    SchoolModel Base() {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"}, {41, "TB"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      return m;
    }

    LessonInstance Lesson(Id id, Id subject, Id teacher) {
      LessonInstance l;
      l.id = id;
      l.participants = { {20, kNoId} };
      l.subject_id = subject;
      l.teacher_id = teacher;
      l.requires_room = false;
      return l;
    }

    bool HasKind(const InfeasibleCore& core, const char* kind,
      const char* rule = nullptr) {
      return std::any_of(core.items.begin(), core.items.end(),
        [&] (const CoreItem& item) {
          return item.kind == kind &&
            (rule == nullptr || item.rule == rule);
        });
    }

    SolveParams Params() {
      return { .max_time_seconds = 10.0, .num_workers = 1, .random_seed = 7 };
    }

    // Two locks leave a 2-period hole; gap_windows is HARD by default. The
    // core must name both locks AND the rule — relaxing any one of the three
    // makes the model solvable.
    TEST(Explain, LocksVersusHardWindowRule) {
      SchoolModel m = Base();
      m.lessons = { Lesson(100, 50, 40), Lesson(101, 51, 41) };
      m.lessons[0].locked = true;
      m.lessons[0].locked_placement = Placement{ 1, 0, kNoId };
      m.lessons[1].locked = true;
      m.lessons[1].locked_placement = Placement{ 1, 3, kNoId };
      // Confirm the premise: this really is infeasible.
      ASSERT_EQ(SolveSchedule(m, Params(), nullptr, nullptr).status,
        SolveStatusCode::kInfeasible);

      ModelIndex ix = ModelIndex::Build(m);
      auto core = ExplainInfeasibility(m, ix, {}, Params(), 20.0);
      ASSERT_TRUE(core.has_value());
      EXPECT_TRUE(HasKind(*core, "locked_lesson"));
      EXPECT_TRUE(HasKind(*core, "hard_rule", "gap_windows"));
      EXPECT_FALSE(core->message.empty());
      // Both locks survive shrinking — dropping either one already frees the
      // window, so each is individually necessary.
      int locks = 0;
      for (const CoreItem& item : core->items) {
        locks += item.kind == "locked_lesson";
      }
      EXPECT_EQ(locks, 2);
    }

    // A teacher whose availability leaves fewer slots than their lessons need:
    // the core names that teacher's availability (not the whole world).
    TEST(Explain, AvailabilityCoreNamesTheTeacher) {
      SchoolModel m = Base();
      // TA teaches 5 periods but is blocked for 5 of the 6.
      for (Id id = 100; id < 105; ++id) {
        m.lessons.push_back(Lesson(id, 50, 40));
      }
      m.external_blocks.push_back({ 900, "TA busy", BlockTarget::kTeacher, 40,
        /*day=*/1, /*start=*/0, /*duration=*/5 });
      ModelIndex ix = ModelIndex::Build(m);
      auto core = ExplainInfeasibility(m, ix, {}, Params(), 20.0);
      ASSERT_TRUE(core.has_value());
      EXPECT_TRUE(HasKind(*core, "teacher_availability"));
      bool names_ta = false;
      for (const CoreItem& item : core->items) {
        names_ta |= item.entity_name == "TA";
      }
      EXPECT_TRUE(names_ta);
    }

    // A hard rule alone (no locks, no blocks): subject_once=hard with three
    // same-subject singles and a 2-period... rather: more same-subject runs
    // than one run can hold given another subject pinned mid-day.
    TEST(Explain, HardRuleOnlyCore) {
      SchoolModel m = Base();
      m.days[0].period_count = 3;
      m.lessons = { Lesson(100, 50, 40), Lesson(101, 50, 40),
                   Lesson(102, 51, 41) };
      m.lessons[2].locked = true;
      m.lessons[2].locked_placement = Placement{ 1, 1, kNoId };  // splits the day
      m.rule_config.overrides = { {"subject_once", RuleMode::kHard} };
      ASSERT_EQ(SolveSchedule(m, Params(), nullptr, nullptr).status,
        SolveStatusCode::kInfeasible);
      ModelIndex ix = ModelIndex::Build(m);
      auto core = ExplainInfeasibility(m, ix, {}, Params(), 20.0);
      ASSERT_TRUE(core.has_value());
      EXPECT_TRUE(HasKind(*core, "hard_rule", "subject_once"));
      EXPECT_TRUE(HasKind(*core, "locked_lesson"));
    }

    // Impossible without any locks/blocks/rules (more lessons than slots): the
    // explanation names the DIVISION, either via its completeness switch or
    // its (built-in) daily bounds — both are irreducible cores of this model,
    // and deletion finds one of them.
    TEST(Explain, CompletenessCoreWhenNothingElseHelps) {
      SchoolModel m = Base();
      m.days[0].period_count = 2;
      m.periods.resize(2);
      for (Id id = 100; id < 103; ++id) {
        m.lessons.push_back(Lesson(id, 50, 40));  // 3 lessons, 2 slots
      }
      // Premise check: genuinely infeasible.
      ASSERT_EQ(SolveSchedule(m, Params(), nullptr, nullptr).status,
        SolveStatusCode::kInfeasible);
      ModelIndex ix = ModelIndex::Build(m);
      auto core = ExplainInfeasibility(m, ix, {}, Params(), 20.0);
      ASSERT_TRUE(core.has_value());
      EXPECT_TRUE(HasKind(*core, "placement_completeness") ||
        HasKind(*core, "daily_bounds"));
      bool names_division = false;
      for (const CoreItem& item : core->items) {
        names_division |= item.entity_name == "1A";
      }
      EXPECT_TRUE(names_division);
    }

    TEST(Explain, TimeoutHintsNameHeavyInputs) {
      SchoolModel m = Base();
      // TA booked 6 of 6 periods -> 100% teacher hint.
      for (Id id = 100; id < 106; ++id) m.lessons.push_back(Lesson(id, 50, 40));
      ModelIndex ix = ModelIndex::Build(m);
      std::vector<CoreItem> hints = TimeoutHints(m, ix);
      bool ta = false;
      for (const CoreItem& hint : hints) {
        ta |= hint.kind == "hint" && hint.entity_name == "TA";
      }
      EXPECT_TRUE(ta);
    }

  }  // namespace
}  // namespace arrango
