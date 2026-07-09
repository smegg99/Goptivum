// src/solve/cpsat_runner.h

#pragma once

#include <atomic>
#include <functional>
#include <vector>

#include "model/model.h"
#include "ortools/sat/cp_model.h"
#include "solve/candidates.h"
#include "solve/solver.h"

namespace arrango {

  struct RunOptions {
    double max_time_seconds{};
    int num_workers{};      // 0 = all available cores
    int64_t random_seed{ 1 };
  };

  // Solves an already-built CP-SAT model `cp` whose decision layer is `x` over
  // candidates `cs`, streaming improved solutions (`on_improved`) and ~1 Hz
  // heartbeats (`on_progress`), then maps the response to a SolveResult whose
  // `best` snapshot is extracted from `x`. `elapsed` reports cumulative wall time
  // so a multi-phase pipeline reports one clock; `cancel` (optional) aborts.
  SolveResult RunSearch(operations_research::sat::CpModelBuilder& cp,
    const SchoolModel& m, const CandidateSet& cs,
    const std::vector<operations_research::sat::BoolVar>& x,
    const RunOptions& opts, std::atomic<bool>* cancel,
    const ProgressFn& on_improved,
    const HeartbeatFn& on_progress,
    const std::function<double()>& elapsed);

}  // namespace arrango
