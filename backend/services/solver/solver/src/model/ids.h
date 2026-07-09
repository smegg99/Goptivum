// src/model/ids.h

#pragma once

#include <cstdint>

namespace arrango {

  using Id = std::uint32_t;
  inline constexpr Id kNoId = 0;

}  // namespace arrango
