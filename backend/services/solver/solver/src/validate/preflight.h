// src/validate/preflight.h

#pragma once

#include <cstdint>

#include "model/index.h"
#include "model/model.h"
#include "score/rules.h"
#include "validate/conflict.h"

namespace arrango {

  // Input impossibilities caught BEFORE any CP-SAT work, each naming its
  // culprit. `hard` findings make the solve refuse immediately; `advisory`
  // findings are streamed to the caller and the solve proceeds. Findings
  // reuse Conflict, so every consumer already knows how to render them.
  struct PreflightReport {
    std::vector<Conflict> hard;
    std::vector<Conflict> advisory;
  };

  // max_streams_per_division: 0 = default 64 (the stream-cap check lives
  // here; solver.cc consumes the report instead of checking itself).
  // The resolver's config diagnostics (unknown rule/profile/scope, hard on
  // a non-hardable rule) become hard findings — a mistyped philosophy must
  // refuse loudly, not silently fall back to defaults.
  PreflightReport RunPreflight(const SchoolModel& m, const ModelIndex& ix,
    uint32_t max_streams_per_division, const RuleResolver& rules);

}  // namespace arrango
