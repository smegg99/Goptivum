// src/solve/solver.h

#pragma once

#include <atomic>
#include <cstdint>
#include <functional>
#include <string>

#include "model/eligibility.h"
#include "model/model.h"

namespace arrango {

  enum class SolveStatusCode {
    kOptimal,
    kFeasible,
    kInfeasible,
    kCancelled,
    kTimeout,
    kError,
  };

  // Which pipeline stage(s) to run. Only the full pipeline is implemented;
  // asking for an individual stage returns a clear error.
  enum class SolveMode {
    kFullPipeline,
    kViabilityCheck,
    kConstructFast,
    kConstructClean,
    kAssignRooms,
    kRepair,
    kPolish,
  };

  enum class UnplacedPolicy { kForbid, kAllowPenalized };
  enum class PoolStrategy { kAuto, kExactOnly, kPoolLarge };
  enum class PoolSafety { kStrict, kMatchingCuts, kReportRepair };

  struct PhaseLimits {
    double construct_fast_seconds{};
    double construct_clean_seconds{};
    double assign_rooms_seconds{};
    double repair_seconds{};
    double polish_seconds{};
    bool operator==(const PhaseLimits&) const = default;
  };

  // Fully-resolved runtime solver configuration. Every solve result echoes the
  // config that produced it (the config snapshot results are compared by).
  // Several knobs are reserved for the staged pipeline and are echoed but
  // ignored for now; mode, time, workers, seed, LNS, capacity policy, rules
  // and weights all take effect today.
  struct SolverConfig {
    SolveMode mode{ SolveMode::kFullPipeline };
    double total_time_seconds{ 180.0 };
    PhaseLimits phase_limits{};
    int num_workers{};  // 0 = all available cores
    int64_t random_seed{ 1 };
    uint32_t exact_room_threshold{ 3 };
    uint32_t max_candidates_per_lesson{};
    uint32_t max_streams_per_division{};  // 0 = built-in default (64)
    bool disable_warm_start{};
    uint32_t portfolio_seeds{};
    bool disable_lns{};
    uint32_t lns_iterations{ 30 };
    // 0 = auto: the pipeline derives a per-neighborhood slice from the time
    // budget (~4%, clamped to [3s, 15s]). A fixed 2s was too short to optimize
    // a neighborhood past presolve, so LNS made no progress on large models.
    double lns_seconds_per_neighborhood{ 0.0 };
    PoolStrategy pool_strategy{ PoolStrategy::kAuto };
    PoolSafety pool_safety{ PoolSafety::kStrict };
    UnknownCapacityPolicy unknown_capacity_policy{ UnknownCapacityPolicy::kAllow };
    UnplacedPolicy unplaced_policy{ UnplacedPolicy::kForbid };
    bool ignore_previous_placements{};
    bool ignore_viability_block{};
    Weights weights{};
    int64_t unplaced_penalty{ 1000000 };
    // Echo-only on updates: the effective rule layers (model's, then the
    // request's) so every stored result records the philosophy behind it.
    RuleConfig rule_config{};
    // The explainer runs after INFEASIBLE by default; it costs extra time
    // (bounded by explain_budget_seconds; 0 = auto 25% clamp [5s, 30s]).
    bool disable_infeasibility_explainer{};
    double explain_budget_seconds{};
    bool operator==(const SolverConfig&) const = default;
  };

  struct SolveParams {
    double max_time_seconds{ 20.0 };
    int num_workers{ 1 };  // 0 = all available cores
    int64_t random_seed{ 1 };
    UnknownCapacityPolicy unknown_capacity_policy{ UnknownCapacityPolicy::kAllow };
    // Construct+LNS pipeline (engaged only on models too large for a direct
    // optimize). Zero = use the built-in default.
    bool disable_lns{ false };             // stop after the feasible construct
    double lns_seconds_per_neighborhood{ 0.0 };  // 0 = auto from the time budget
    uint32_t lns_neighborhood_divisions{ 0 };    // 0 = default (3)
    // Caps the room dimension per lesson when the model would otherwise be
    // intractable (unbounded rooms). 0 = auto (a default is applied only when
    // the uncapped candidate set is far too large).
    uint32_t max_candidates_per_lesson{ 0 };
    // Skip CP-SAT placement hints from previous placements. Unlike clearing
    // the placements themselves, the stability penalty and the never-worse
    // baseline still apply — only the search no longer starts from them.
    bool disable_warm_start{ false };
    // Refuse models whose fixed splits multiply into more student streams
    // than this per division (see model/structure.h). 0 = default (64) —
    // beyond that the model is almost certainly mis-declared.
    uint32_t max_streams_per_division{ 0 };
    // INTERNAL (not exposed on the API): LNS sub-solves set this — their
    // models derive from an already-validated incumbent, so re-running the
    // preflight per neighborhood is pure waste.
    bool skip_preflight{ false };
    // Request-level rule overrides, layered over the model's rule_config
    // (score/rules.h resolves the stack).
    RuleConfig rule_config;
  };

  struct SolveResult {
    SolveStatusCode status{ SolveStatusCode::kError };
    ScheduleSnapshot best;
    int64_t objective{};
    int64_t best_bound{};   // proven lower bound on the objective
    int solutions_found{};
    double wall_time_seconds{};
    std::string message;
  };

  // Called for every improved feasible solution found during search.
  using ProgressFn = std::function<void(const SolveResult&)>;

  // Lightweight search heartbeat: objective/bound/solution counters only,
  // no snapshot. Called roughly once per second from a helper thread.
  using HeartbeatFn = std::function<void(const SolveResult&)>;

  // What the solver is doing right now — mirrors proto SolveStage.
  enum class SolveStage {
    kUnspecified,
    kPreflight,
    kCandidates,
    kConstruct,
    kDirect,
    kLns,
    kValidate,
    kDone,
    kExplain,  // proving WHY the model is infeasible
  };

  // One live status snapshot; emitted on stage transitions and per LNS
  // neighborhood so the caller is never left on a silent stream.
  struct SolveProgressInfo {
    SolveStage stage{ SolveStage::kUnspecified };
    std::string detail;
    uint64_t candidates{};
    uint32_t lns_pass{};
    uint32_t lns_neighborhood{};
    uint32_t lns_neighborhoods_total{};
    uint32_t lns_accepted{};
    uint32_t lns_rejected_worse{};
    uint32_t lns_rejected_invalid{};
    double stage_elapsed_seconds{};
  };
  using StageSink = std::function<void(const SolveProgressInfo&)>;

  // Builds and solves the CP-SAT model: pruned candidate booleans
  // x[lesson, day, start, room], exactly-one per lesson, no-overlap per
  // teacher/room/class/group, parallel-block channeling, soft objective.
  // `cancel` (optional) aborts the search when set; the best feasible
  // solution found so far is returned with status kCancelled.
  SolveResult SolveSchedule(const SchoolModel& model, const SolveParams& params,
    std::atomic<bool>* cancel,
    const ProgressFn& on_improved,
    const HeartbeatFn& on_progress = nullptr,
    const StageSink& on_stage = nullptr);

}  // namespace arrango
