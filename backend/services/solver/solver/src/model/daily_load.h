// src/model/daily_load.h

#pragma once

#include <cstdint>
#include <vector>

#include "model/atoms.h"
#include "model/index.h"
#include "model/model.h"

namespace arrango {

  // A resolved per-day lesson-load rule. min/max are HARD bounds on the
  // WHOLE DIVISION (periods the division is in session that day, parallel
  // splits counted once); the four weights are SOFT per-stream penalties
  // against target_per_day (see score/penalty_defs.h DailyLoadDayCounts).
  struct EffectiveDailyLoad {
    uint32_t min_per_day{};     // after auto-relax; 0 = none
    uint32_t max_per_day{};     // 0 = none
    uint32_t target_per_day{};  // resolved (auto -> ceil(weekly / active days))
    uint32_t allowed_deviation{};
    int64_t deviation_weight{};
    int64_t imbalance_weight{};
    int64_t overload_weight{};
    int64_t underload_weight{};
  };

  // Resolves the effective rule for one DIVISION. Most specific rule wins: a
  // division rule (matching the division) beats the school default
  // (division_id == kNoId) beats the built-in default {min 3, no max, auto
  // target}. Group-scoped rules are ignored here — the minimum is a
  // division-level quantity. The minimum auto-relaxes to
  // min(configured, floor(weekly / active_days)) where weekly is the
  // LEAST-loaded atom's periods — a heavier minimum could never be met by
  // the lightest student set, making every schedule infeasible.
  EffectiveDailyLoad ResolveDailyLoad(const SchoolModel& m, const ModelIndex& ix,
    const AtomSet& atoms, int division_idx);

  // [division_idx][day_idx] = number of periods the division is in session on
  // that day (union over all the division's lessons; parallel splits count
  // once).
  std::vector<std::vector<uint32_t>> DivisionDailyLoads(const SchoolModel& m,
    const ModelIndex& ix,
    const ScheduleSnapshot& s);

}  // namespace arrango
