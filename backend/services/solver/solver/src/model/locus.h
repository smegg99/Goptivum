// src/model/locus.h

#pragma once

#include <cstdint>
#include <vector>

#include "model/ids.h"

namespace arrango {

  // Structured, language-agnostic localization shared by hard conflicts and
  // soft issues. Consumers render and highlight issues from these refs plus the
  // issue's kind/category — never from a prose message.

  enum class EntityKind {
    kUnspecified,
    kDivision,
    kGroup,
    kTeacher,
    kRoom,
    kSubject,
    kYear,
    kExternalBlock,
  };

  // A typed reference to a model entity involved in an issue.
  struct EntityRef {
    EntityKind kind{ EntityKind::kUnspecified };
    Id id{ kNoId };
    bool operator==(const EntityRef&) const = default;
  };

  // An affected lesson block and where it sits in the schedule being reported
  // on. day_id / start_period / room_id are kNoId/0 when the lesson is unplaced.
  struct LessonRef {
    Id lesson_id{ kNoId };
    Id day_id{ kNoId };
    uint32_t start_period{};
    uint32_t duration{ 1 };
    Id room_id{ kNoId };
    bool operator==(const LessonRef&) const = default;
  };

  // An affected run of periods on one day. period_span >= 1.
  struct TimeSpan {
    Id day_id{ kNoId };
    uint32_t start_period{};
    uint32_t period_span{ 1 };
    bool operator==(const TimeSpan&) const = default;
  };

  // A mixin of the three structured-locus vectors, embedded in Conflict and
  // SoftIssue so both localize the same way.
  struct IssueLocus {
    std::vector<EntityRef> entities;
    std::vector<LessonRef> lessons;
    std::vector<TimeSpan> spans;
    bool operator==(const IssueLocus&) const = default;
  };

}  // namespace arrango
