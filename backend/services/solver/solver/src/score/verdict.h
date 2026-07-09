// src/score/verdict.h

#pragma once

#include <cstdint>

#include "score/scorer.h"
#include "validate/conflict.h"  // types only: no link on arrango_validate

namespace arrango {

  // The one-glance answer "is this schedule pristine?". Composed from the
  // validator (errors) and the scorer's located issues (warnings), so the
  // rating and the validation can never tell different stories:
  //   ERRORS   — any hard conflict;
  //   WARNINGS — any comfort issue in a designated category (the metric
  //              table's categories, students AND teachers);
  //   PRISTINE — neither. INFO findings never affect the tier.
  enum class VerdictTier { kPristine, kWarnings, kErrors };

  struct ScheduleVerdict {
    VerdictTier tier{ VerdictTier::kPristine };
    uint32_t errors{};             // structural + config-hard combined
    uint32_t warnings{};
    uint32_t infos{};
    // Of `errors`, how many are config-hard rule violations (as opposed to
    // structural illegality).
    uint32_t config_hard_errors{};
  };

  ScheduleVerdict ComposeVerdict(const ValidationReport& validation,
    const ScoreReport& score);

}  // namespace arrango
