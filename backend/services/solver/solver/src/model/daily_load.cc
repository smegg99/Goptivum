// src/model/daily_load.cc

#include "model/daily_load.h"

#include <algorithm>

namespace arrango {
  namespace {

    int ActiveDays(const SchoolModel& m) {
      int n = 0;
      for (const Day& d : m.days) {
        if (d.period_count >= 1) ++n;
      }
      return n;
    }

    // Weekly periods of one atom: sum of durations of lessons covering it.
    int64_t AtomWeekly(const SchoolModel& m, const AtomSet& atoms, int atom_idx) {
      int64_t total = 0;
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        const std::vector<int>& of = atoms.of_lesson[li];
        if (std::binary_search(of.begin(), of.end(), atom_idx)) {
          total += m.lessons[li].duration;
        }
      }
      return total;
    }

  }  // namespace

  EffectiveDailyLoad ResolveDailyLoad(const SchoolModel& m, const ModelIndex& ix,
    const AtomSet& atoms, int division_idx) {
    const Id division_id = m.divisions[division_idx].id;

    EffectiveDailyLoad eff;

    // Most specific rule: a division rule for this division beats the school
    // default (division_id == kNoId). Group-scoped rules do not apply to the
    // division minimum.
    const DailyLoadRule* chosen = nullptr;
    int chosen_rank = -1;  // 0 default, 1 division
    for (const DailyLoadRule& rule : m.daily_load_rules) {
      if (rule.group_id != kNoId) continue;  // group rules ignored for min/max
      int rank;
      if (rule.division_id == kNoId) {
        rank = 0;  // school default
      }
      else if (rule.division_id == division_id) {
        rank = 1;  // this division
      }
      else {
        continue;
      }
      if (rank > chosen_rank) {
        chosen_rank = rank;
        chosen = &rule;
      }
    }

    if (chosen != nullptr) {
      eff.min_per_day = chosen->min_per_day;  // 0 = explicitly no minimum
      eff.max_per_day = chosen->max_per_day;
      eff.target_per_day = chosen->target_per_day;
      eff.allowed_deviation = chosen->allowed_deviation;
      eff.deviation_weight = chosen->deviation_weight;
      eff.imbalance_weight = chosen->imbalance_weight;
      eff.overload_weight = chosen->overload_weight;
      eff.underload_weight = chosen->underload_weight;
    }
    else {
      eff.min_per_day = 3;  // built-in default
    }

    // Auto-relax by the LEAST-loaded atom: the minimum applies to the whole
    // division on every active day, so it can never exceed what the lightest
    // student's weekly load can spread over those days. weekly = min over the
    // division's atoms of that atom's weekly periods.
    int64_t weekly = -1;
    for (int a : atoms.of_division[division_idx]) {
      int64_t w = AtomWeekly(m, atoms, a);
      if (weekly < 0 || w < weekly) weekly = w;
    }
    if (weekly < 0) weekly = 0;
    const int64_t days = ActiveDays(m);

    if (days > 0) {
      int64_t feasible_min = weekly / days;  // floor
      eff.min_per_day = static_cast<uint32_t>(
        std::min<int64_t>(eff.min_per_day, feasible_min));
    }
    else {
      eff.min_per_day = 0;
    }

    if (eff.target_per_day == 0 && days > 0) {
      eff.target_per_day = static_cast<uint32_t>((weekly + days - 1) / days);
    }

    return eff;
  }

  std::vector<std::vector<uint32_t>> DivisionDailyLoads(
    const SchoolModel& m, const ModelIndex& ix, const ScheduleSnapshot& s) {
    const size_t nd = m.days.size();
    // Track occupied periods per (division, day) as a set to avoid double
    // counting parallel splits.
    std::vector<std::vector<std::vector<bool>>> occupied(m.divisions.size());
    for (auto& v : occupied) {
      v.assign(nd, {});
      for (size_t d = 0; d < nd; ++d) v[d].assign(m.days[d].period_count, false);
    }
    for (const ScheduledLesson& sl : s.lessons) {
      int li = ix.LessonIdx(sl.lesson_id);
      if (li < 0) continue;
      int di = ix.DayIdx(sl.placement.day_id);
      if (di < 0) continue;
      const LessonInstance& lesson = m.lessons[li];
      for (const Participant& p : lesson.participants) {
        int c = ix.DivisionIdx(p.division_id);
        if (c < 0) continue;
        for (uint32_t q = sl.placement.start_period;
          q < sl.placement.start_period + lesson.duration &&
          q < m.days[di].period_count;
          ++q) {
          occupied[c][di][q] = true;
        }
      }
    }
    std::vector<std::vector<uint32_t>> loads(
      m.divisions.size(), std::vector<uint32_t>(nd, 0));
    for (size_t c = 0; c < m.divisions.size(); ++c) {
      for (size_t d = 0; d < nd; ++d) {
        uint32_t count = 0;
        for (bool b : occupied[c][d]) count += b ? 1 : 0;
        loads[c][d] = count;
      }
    }
    return loads;
  }

}  // namespace arrango
