// tests/lns_test.cc

#include <gtest/gtest.h>

#include <atomic>
#include <chrono>

#include "score/scorer.h"
#include "solve/lns.h"
#include "validate/validator.h"

namespace arrango {
  namespace {

    // LNS frees a few divisions and holds every other lesson fixed. These tests
    // pin the two ways a single-division neighborhood used to corrupt a schedule:
    // a cross-division MERGED lesson moved without the other division's students
    // in the sub-model, and a cross-division PARALLEL BLOCK member moved away
    // from its fixed sibling. In both fixtures the soft-optimal move for the
    // freed division alone is exactly the corrupting one, so an unsafe LNS is
    // guaranteed to take it.

    constexpr Id kDay = 1;
    constexpr Id kYear = 10, kDivA = 20, kDivB = 21;
    constexpr Id kTeachA = 40, kTeachB = 41, kTeachM = 42, kTeachM2 = 43;
    constexpr Id kSub = 50;

    // One day, six periods, two divisions, roomless lessons. Division A holds
    // locked singles at periods 0 and 2; division B holds locked singles at
    // 1, 2, 3. The movable lesson sits at period 4, leaving A gaps at 1 and 3
    // that only a move to period 1 (on top of B's students) can fully close.
    SchoolModel TwoDivisionModel() {
      SchoolModel m;
      m.days = { { kDay, "Pon", 6 } };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { { kYear, "Rok 1", 1, 100 } };
      m.divisions = { { kDivA, "1A", kYear }, { kDivB, "1B", kYear } };
      m.teachers = { { kTeachA, "TA" }, { kTeachB, "TB" }, { kTeachM, "TM" },
                    { kTeachM2, "TM2" } };
      m.subjects = { { kSub, "Mat" } };
      return m;
    }

    LessonInstance LockedSingle(Id id, Id division, Id teacher, uint32_t period) {
      LessonInstance l;
      l.id = id;
      l.participants = { { division, kNoId } };
      l.subject_id = kSub;
      l.teacher_id = teacher;
      l.requires_room = false;
      l.locked = true;
      l.locked_placement = Placement{ kDay, period, kNoId };
      return l;
    }

    void AddLockedFixture(SchoolModel& m, ScheduleSnapshot& s) {
      for (auto [id, division, teacher, period] :
        { std::tuple<Id, Id, Id, uint32_t>{ 100, kDivA, kTeachA, 0 },
         { 101, kDivA, kTeachA, 2 },
         { 102, kDivB, kTeachB, 1 },
         { 103, kDivB, kTeachB, 2 },
         { 104, kDivB, kTeachB, 3 } }) {
        m.lessons.push_back(LockedSingle(id, division, teacher, period));
        s.lessons.push_back({ id, Placement{ kDay, period, kNoId } });
      }
    }

    SolveParams SingleDivisionBudget() {
      SolveParams p;
      p.max_time_seconds = 4.0;
      p.num_workers = 1;
      p.random_seed = 7;
      p.lns_seconds_per_neighborhood = 0.5;
      p.lns_neighborhood_divisions = 1;  // free one division at a time
      return p;
    }

    ScheduleSnapshot Polish(const SchoolModel& m, const ScheduleSnapshot& start,
      const SolveParams& p) {
      const auto t0 = std::chrono::steady_clock::now();
      return PolishByLns(m, start, p, nullptr, nullptr, [&t0] {
        return std::chrono::duration<double>(std::chrono::steady_clock::now() - t0)
          .count();
        });
    }

    TEST(Lns, CrossDivisionMergeNeverCreatesStudentOverlap) {
      SchoolModel m = TwoDivisionModel();
      ScheduleSnapshot start;
      AddLockedFixture(m, start);
      LessonInstance merged;
      merged.id = 110;
      merged.participants = { { kDivA, kNoId }, { kDivB, kNoId } };
      merged.subject_id = kSub;
      merged.teacher_id = kTeachM;
      merged.requires_room = false;
      m.lessons.push_back(merged);
      start.lessons.push_back({ 110, Placement{ kDay, 4, kNoId } });

      ASSERT_TRUE(Validate(m, start).valid);
      const int64_t start_penalty = ComputeScore(m, start).total_penalty;

      ScheduleSnapshot polished = Polish(m, start, SingleDivisionBudget());

      ValidationReport report = Validate(m, polished);
      EXPECT_TRUE(report.valid)
        << (report.conflicts.empty() ? "" : report.conflicts[0].message);
      EXPECT_LE(ComputeScore(m, polished).total_penalty, start_penalty);
    }

    TEST(Lns, CrossDivisionParallelBlockStaysAligned) {
      SchoolModel m = TwoDivisionModel();
      ScheduleSnapshot start;
      AddLockedFixture(m, start);
      for (auto [id, division, teacher] :
        { std::tuple<Id, Id, Id>{ 110, kDivA, kTeachM }, { 111, kDivB, kTeachM2 } }) {
        LessonInstance member;
        member.id = id;
        member.participants = { { division, kNoId } };
        member.subject_id = kSub;
        member.teacher_id = teacher;
        member.requires_room = false;
        member.parallel_block_id = 5;
        m.lessons.push_back(member);
        start.lessons.push_back({ id, Placement{ kDay, 4, kNoId } });
      }

      ASSERT_TRUE(Validate(m, start).valid);
      const int64_t start_penalty = ComputeScore(m, start).total_penalty;

      ScheduleSnapshot polished = Polish(m, start, SingleDivisionBudget());

      ValidationReport report = Validate(m, polished);
      EXPECT_TRUE(report.valid)
        << (report.conflicts.empty() ? "" : report.conflicts[0].message);
      EXPECT_LE(ComputeScore(m, polished).total_penalty, start_penalty);
    }

    // The reduced model must carry the freed divisions' splits: without them the
    // sub-model's streams collapse to flat groups, it cannot see that moving the
    // girls' lesson to period 2 closes the (o1 ∩ girls) gap, and LNS never finds
    // the improvement.
    TEST(Lns, ReducedModelKeepsSplitSemantics) {
      SchoolModel m;
      m.days = { { kDay, "Pon", 4 } };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { { kYear, "Rok 1", 1, 100 } };
      m.divisions = { { kDivA, "1A", kYear } };
      m.teachers = { { kTeachA, "TA" }, { kTeachB, "TB" } };
      m.subjects = { { kSub, "Mat" } };
      m.splits = { { 5, "lang", kDivA, SplitKind::kOpen },
                  { 7, "pe", kDivA, SplitKind::kFixed } };
      m.groups = { {.id = 30, .name = "o1", .division_id = kDivA, .split_id = 5 },
                  {.id = 33, .name = "girls", .division_id = kDivA, .split_id = 7 },
                  {.id = 34, .name = "boys", .division_id = kDivA, .split_id = 7 } };
      ScheduleSnapshot start;
      // o1 locked at periods 1 and 3 -> gap at 2 for both product streams.
      for (auto [id, period] : { std::pair<Id, uint32_t>{ 100, 1 }, { 101, 3 } }) {
        LessonInstance l;
        l.id = id;
        l.participants = { { kDivA, 30 } };
        l.subject_id = kSub;
        l.teacher_id = kTeachA;
        l.requires_room = false;
        l.locked = true;
        l.locked_placement = Placement{ kDay, period, kNoId };
        m.lessons.push_back(l);
        start.lessons.push_back({ id, Placement{ kDay, period, kNoId } });
      }
      LessonInstance pe;  // movable; at period 2 it fills the girls' gap
      pe.id = 110;
      pe.participants = { { kDivA, 33 } };
      pe.subject_id = kSub;
      pe.teacher_id = kTeachB;
      pe.requires_room = false;
      m.lessons.push_back(pe);
      start.lessons.push_back({ 110, Placement{ kDay, 0, kNoId } });

      ASSERT_TRUE(Validate(m, start).valid);
      const int64_t start_penalty = ComputeScore(m, start).total_penalty;

      ScheduleSnapshot polished = Polish(m, start, SingleDivisionBudget());

      EXPECT_TRUE(Validate(m, polished).valid);
      EXPECT_LT(ComputeScore(m, polished).total_penalty, start_penalty)
        << "LNS failed to close the girls-stream gap";
    }

    // The stage sink narrates every neighborhood: accepted counters move and
    // events carry the freed division names.
    TEST(Lns, StageSinkReportsNeighborhoods) {
      SchoolModel m;
      m.days = { { kDay, "Pon", 4 } };
      for (Id p = 1; p <= 4; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { { kYear, "Rok 1", 1, 100 } };
      m.divisions = { { kDivA, "1A", kYear } };
      m.teachers = { { kTeachA, "TA" }, { kTeachB, "TB" } };
      m.subjects = { { kSub, "Mat" } };
      ScheduleSnapshot start;
      for (auto [id, period] : { std::pair<Id, uint32_t>{ 100, 1 }, { 101, 3 } }) {
        LessonInstance l;
        l.id = id;
        l.participants = { { kDivA, kNoId } };
        l.subject_id = kSub;
        l.teacher_id = kTeachA;
        l.requires_room = false;
        l.locked = true;
        l.locked_placement = Placement{ kDay, period, kNoId };
        m.lessons.push_back(l);
        start.lessons.push_back({ id, Placement{ kDay, period, kNoId } });
      }
      LessonInstance movable;  // at period 2 it fills the whole-class gap
      movable.id = 110;
      movable.participants = { { kDivA, kNoId } };
      movable.subject_id = kSub;
      movable.teacher_id = kTeachB;
      movable.requires_room = false;
      m.lessons.push_back(movable);
      start.lessons.push_back({ 110, Placement{ kDay, 0, kNoId } });

      std::vector<SolveProgressInfo> events;
      const auto t0 = std::chrono::steady_clock::now();
      PolishByLns(m, start, SingleDivisionBudget(), nullptr, nullptr,
        [&t0] {
          return std::chrono::duration<double>(
            std::chrono::steady_clock::now() - t0)
            .count();
        },
        [&] (const SolveProgressInfo& info) { events.push_back(info); });

      ASSERT_FALSE(events.empty());
      bool accepted = false;
      for (const SolveProgressInfo& e : events) {
        EXPECT_EQ(e.stage, SolveStage::kLns);
        EXPECT_GE(e.lns_neighborhoods_total, 1u);
        accepted |= e.lns_accepted > 0 &&
          e.detail.find("1A") != std::string::npos;
      }
      EXPECT_TRUE(accepted);
    }

    // A construct incumbent can violate a HARD rule (hard encodings live with
    // the objective, which construct skips). Hard violations carry ZERO
    // penalty by design, so penalty-only acceptance would never repair them —
    // acceptance must be lexicographic: (config-hard violations, penalty).
    TEST(Lns, RepairsConfigHardViolationsEvenAtEqualPenalty) {
      SchoolModel m;
      m.days = { {1, "Pon", 3}, {2, "Wt", 3} };
      for (Id p = 1; p <= 3; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {kYear, "Rok 1", 1, 100} };
      m.divisions = { {kDivA, "1A", kYear} };
      m.teachers = { {kTeachA, "TA"} };
      m.subjects = { {kSub, "Mat"}, {51, "Ang"} };
      m.daily_load_rules = { {.division_id = kNoId, .min_per_day = 0} };
      m.rule_config.overrides = { {"subject_once", RuleMode::kHard} };
      for (auto [id, subject] :
        { std::pair<Id, Id>{100, kSub}, {101, Id{51}}, {102, kSub} }) {
        LessonInstance l;
        l.id = id;
        l.participants = { {kDivA, kNoId} };
        l.subject_id = subject;
        l.teacher_id = kTeachA;
        l.requires_room = false;
        m.lessons.push_back(l);
      }
      // Incumbent: Mat, Ang, Mat on one day — gap-free, zero soft penalty,
      // but a config-hard subject double.
      ScheduleSnapshot start{ {{100, {1, 0, kNoId}},
                              {101, {1, 1, kNoId}},
                              {102, {1, 2, kNoId}}} };
      auto hard_count = [&] (const ScheduleSnapshot& s) {
        int64_t count = 0;
        for (const SoftIssue& issue : ComputeScore(m, s).soft_issues) {
          if (issue.config_hard) count += issue.count;
        }
        return count;
        };
      ASSERT_GT(hard_count(start), 0);

      ScheduleSnapshot polished = Polish(m, start, SingleDivisionBudget());
      EXPECT_TRUE(Validate(m, polished).valid);
      EXPECT_EQ(hard_count(polished), 0) << "LNS left a hard violation in place";
    }

  }  // namespace
}  // namespace arrango
