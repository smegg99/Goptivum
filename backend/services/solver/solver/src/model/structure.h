// src/model/structure.h

#pragma once

#include <cstdint>
#include <vector>

#include "model/index.h"
#include "model/model.h"

namespace arrango {

  // THE single source of student-overlap semantics. Everything that asks
  // "could these two student sets contain the same kid?" — candidate pruning,
  // hard constraints, the validator, LNS blocks, stream building — must go
  // through here, so the answer can never drift between modules.
  //
  // The whole rulebook:
  //   1. different groups of the SAME split       -> do not share (parallel OK)
  //   2. different splits, both OPEN              -> do not share (parallel OK)
  //   3. different splits, at least one FIXED     -> share (never parallel)
  //   4. whole division vs anything, same group   -> share
  //      different divisions                      -> never share
  //
  // Unknown ids resolve to "no shared students"; the validator reports them.
  bool SharesStudents(const SchoolModel& m, const ModelIndex& ix,
    const Participant& a, const Participant& b);

  // Student-stream count per division under the open-group x fixed-tuple
  // stream model (see model/atoms.cc). 0 = "one whole-class stream". Used by
  // the solver preflight to reject models whose fixed splits multiply into
  // absurd stream counts before any CP-SAT work happens.
  std::vector<int64_t> StreamCountPerDivision(const SchoolModel& m,
    const ModelIndex& ix);

}  // namespace arrango
