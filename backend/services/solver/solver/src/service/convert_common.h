// src/service/convert_common.h

#pragma once

#include "service/convert.h"

namespace arrango {

  // Placement <-> proto, shared by model (lesson locked/previous placements) and
  // snapshot conversion.
  Placement PlacementFromProto(const v1::Placement& in);
  void PlacementToProto(const Placement& in, v1::Placement* out);

}  // namespace arrango
