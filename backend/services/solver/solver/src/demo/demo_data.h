// src/demo/demo_data.h

#pragma once

#include <cstdint>

#include "model/model.h"

namespace arrango {

  // Only two presets remain: the production replica and its feature-complete
  // bigger sibling. Small models for tests are hand-built fixtures instead
  // (tests/test_school.h).
  enum class DemoPreset { kProduction, kMega };

  // Deterministic synthetic Polish technical-school model. Same preset+seed
  // always yields an identical model; the seed only rotates name/assignment
  // choices, never structure sizes.
  SchoolModel GenerateDemoSchool(DemoPreset preset, uint64_t seed);

}  // namespace arrango
