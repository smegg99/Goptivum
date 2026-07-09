// src/validate/conflict.h

#pragma once

#include <string>
#include <vector>

#include "model/ids.h"
#include "model/locus.h"

namespace arrango {

  enum class ConflictKind {
    kTeacherConflict,
    kStudentOverlap,   // two lessons share students at one time
    kRoomConflict,
    kMissingLesson,
    kDuplicateLesson,
    kInvalidRoom,
    kRoomTooSmall,     // known capacity < participant count
    kParallelBlockBroken,
    kLockedMoved,
    kExternalBlockOverlap,
    kOutOfBounds,
    kInvalidReference,
    kInvalidDuration,
    kDailyLoadViolation,   // day under min / over max
    kDuplicateDesignator,  // two rooms share a designator
    // Preflight findings (validate/preflight.h): input impossibilities
    // caught before any CP-SAT work.
    kTeacherOverloaded,    // weekly demand > available slots
    kRoomPoolOverloaded,   // pool demand > pool capacity x week slots
    kLockedCollision,      // two locked lessons collide
    kAvailabilityHole,     // blocked slot inside an otherwise free day
    kStreamExplosion,      // fixed splits multiply past the stream cap
    kGroupSizeMismatch,    // split group sizes exceed the division size
    kInvalidRuleConfig,    // unknown rule/profile/scope, hard on non-hardable
    kLinkSameDay,          // SAME_DAY link members on two days
    kLinkDifferentDay,     // DIFFERENT_DAY link members share a day
    kLinkConsecutive,      // CONSECUTIVE link members not adjacent
    kEdgePlacement,        // edge lesson has same-stream lesson on wrong side
  };

  struct Conflict {
    ConflictKind kind{};
    // Optional debug hint; consumers localize from kind + locus, not this.
    std::string message;
    std::vector<Id> lesson_ids;
    // The conflicting entity (teacher/class/group/room/block), if applicable.
    Id entity_id{ kNoId };
    Id day_id{ kNoId };
    uint32_t period{};
    // Structured localization (typed entities, lesson blocks, time spans).
    IssueLocus locus;
  };

  struct ValidationReport {
    bool valid{};
    std::vector<Conflict> conflicts;
  };

}  // namespace arrango
