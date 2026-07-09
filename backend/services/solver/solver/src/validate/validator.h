// src/validate/validator.h

#pragma once

#include "model/model.h"
#include "validate/conflict.h"

namespace arrango {

  // Pure, CP-SAT-free hard-constraint checker. Never silently ignores an
  // invalid state: every detectable defect becomes a typed Conflict.
  ValidationReport Validate(const SchoolModel& model,
    const ScheduleSnapshot& snapshot);

}  // namespace arrango
