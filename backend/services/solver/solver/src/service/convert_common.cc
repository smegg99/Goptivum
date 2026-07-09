// src/service/convert_common.cc

#include "service/convert_common.h"

namespace arrango {

  Placement PlacementFromProto(const v1::Placement& in) {
    return { in.day_id(), in.start_period(), in.room_id() };
  }

  void PlacementToProto(const Placement& in, v1::Placement* out) {
    out->set_day_id(in.day_id);
    out->set_start_period(in.start_period);
    out->set_room_id(in.room_id);
  }

}  // namespace arrango
