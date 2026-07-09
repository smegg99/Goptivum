// src/solve/lns.h

#pragma once

#include <atomic>
#include <functional>

#include "model/model.h"
#include "solve/solver.h"

namespace arrango {

  // Improves a feasible incumbent by large-neighborhood search: repeatedly frees
  // a small group of divisions, holds every other lesson fixed (as teacher/room/
  // student-set blocks), re-optimizes the freed lessons WITH the soft objective
  // on that small sub-model, and keeps a result only when it lowers the true
  // (full-model) penalty AND passes full hard validation. Cross-division merged
  // lessons and parallel blocks move only when every involved division is freed
  // together; otherwise they stay pinned at their incumbent slot.
  // Runs until `budget.max_time_seconds` of cumulative `elapsed` time or `cancel`.
  // Streams accepted improvements through `on_improved`.
  ScheduleSnapshot PolishByLns(const SchoolModel& m,
    const ScheduleSnapshot& incumbent,
    const SolveParams& budget,
    std::atomic<bool>* cancel,
    const ProgressFn& on_improved,
    const std::function<double()>& elapsed,
    const StageSink& on_stage = nullptr);

}  // namespace arrango
