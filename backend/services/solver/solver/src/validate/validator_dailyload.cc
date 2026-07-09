// src/validate/validator_dailyload.cc

#include <string>
#include <vector>

#include "model/atoms.h"
#include "model/daily_load.h"
#include "validate/validator_internal.h"

namespace arrango {
namespace validating {

// Daily-load rule at the WHOLE-DIVISION level: the division must have at least
// its (auto-relaxed) minimum and at most its maximum lessons on each active day
// — including no empty active day when the weekly load makes the minimum
// feasible. (Per-group attendance parity was removed: real vocational
// timetables run different groups on different days.)
void Checker::CheckDailyLoads() {
  const AtomSet atoms = BuildAtoms(m_, ix_);
  std::vector<std::vector<uint32_t>> div_loads =
      DivisionDailyLoads(m_, ix_, s_);

  // Placed lessons per (division, day) so a daily-load issue can point at the
  // exact blocks of that day.
  std::vector<std::vector<std::vector<LessonRef>>> div_day_lessons(
      m_.divisions.size(),
      std::vector<std::vector<LessonRef>>(m_.days.size()));
  for (const Placed& p : placed_) {
    const LessonInstance& l = m_.lessons[p.lesson_idx];
    for (const Participant& part : l.participants) {
      int dc = ix_.DivisionIdx(part.division_id);
      if (dc < 0) continue;
      div_day_lessons[dc][p.day_idx].push_back(LessonRefFor(l.id));
    }
  }
  auto day_locus = [&] (int c, int d) {
    IssueLocus extra;
    extra.spans.push_back({ m_.days[d].id, 0, m_.days[d].period_count });
    extra.lessons = div_day_lessons[c][d];
    return extra;
    };

  for (size_t c = 0; c < m_.divisions.size(); ++c) {
    EffectiveDailyLoad eff =
        ResolveDailyLoad(m_, ix_, atoms, static_cast<int>(c));
    const Id division_id = m_.divisions[c].id;
    const std::string& name = m_.divisions[c].name;
    for (size_t d = 0; d < m_.days.size(); ++d) {
      if (m_.days[d].period_count < 1) continue;  // inactive day

      uint32_t load = div_loads[c][d];
      if (eff.min_per_day > 0 && load < eff.min_per_day) {
        Add(ConflictKind::kDailyLoadViolation,
          name + ": " + std::to_string(load) +
          " lessons on an active day, below division minimum " +
          std::to_string(eff.min_per_day),
          {}, division_id, m_.days[d].id, 0, EntityKind::kDivision,
          day_locus(static_cast<int>(c), static_cast<int>(d)));
      }
      if (eff.max_per_day > 0 && load > eff.max_per_day) {
        Add(ConflictKind::kDailyLoadViolation,
          name + ": " + std::to_string(load) +
          " lessons on a day, above division maximum " +
          std::to_string(eff.max_per_day),
          {}, division_id, m_.days[d].id, 0, EntityKind::kDivision,
          day_locus(static_cast<int>(c), static_cast<int>(d)));
      }
    }
  }
}

}  // namespace validating
}  // namespace arrango
