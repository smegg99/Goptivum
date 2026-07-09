// src/solve/solver.cc

#include "solve/solver.h"

#include <atomic>
#include <chrono>
#include <cstddef>
#include <variant>
#include <vector>

#include "model/index.h"
#include "ortools/sat/cp_model.h"
#include "score/scorer.h"
#include "solve/candidates.h"
#include "solve/constraints.h"
#include "solve/cpsat_runner.h"
#include "solve/lns.h"
#include "solve/objective.h"
#include "solve/trace.h"
#include "validate/preflight.h"

namespace arrango {
  namespace {

    namespace sat = operations_research::sat;

    // Above this candidate count CP-SAT cannot presolve the full soft-objective
    // model within any usable time budget (measured: 459k candidates / 3.77M
    // clauses on the production instance never leaves presolve). Below it, the
    // direct optimize is fast and exact. Every demo/test preset (<= Full, 54.5k)
    // stays on the direct path; only production-scale models take the construct
    // path.
    constexpr size_t kFullSolveCandidateLimit = 150000;

    // Above this, an unbounded-room model is room-capped: without it CP-SAT
    // can't even construct a feasible schedule (measured: the real Optivum
    // import = 3.76M candidates times out construct). The production preset
    // (459k) stays under this, so it is untouched, as are all LNS sub-models.
    constexpr size_t kRoomCapCandidateLimit = 800000;
    // Per-lesson candidate budget the auto room cap targets (~7-8 rooms/lesson
    // over a 5-day timetable -> ~500k candidates for a 1200-lesson school).
    constexpr uint32_t kDefaultMaxCandidatesPerLesson = 500;

    // Stage reporter bound to the solve's clock; empty detail = just the stage.
    using StageFn =
      std::function<void(SolveStage, std::string, uint64_t /*candidates*/)>;

    // Preflight refusal as a result: proven-impossible inputs (pigeonholes,
    // locked collisions) are INFEASIBLE; misconfigurations (stream explosion,
    // bad rule config) are ERROR.
    SolveResult RefusedByPreflight(const PreflightReport& preflight,
      double elapsed_seconds) {
      SolveResult r;
      r.status = SolveStatusCode::kError;
      for (const Conflict& c : preflight.hard) {
        if (c.kind != ConflictKind::kStreamExplosion &&
          c.kind != ConflictKind::kInvalidRuleConfig) {
          r.status = SolveStatusCode::kInfeasible;
        }
        if (!r.message.empty()) r.message += "; ";
        r.message += c.message;
      }
      r.wall_time_seconds = elapsed_seconds;
      return r;
    }

    // An unbounded-room model (empty designators -> every room eligible) can be
    // intractable; pick a per-lesson room cap when it is. The decision uses a
    // cheap upper-bound estimate so the huge uncapped set is never materialized
    // just to be thrown away. 0 = no cap needed.
    uint32_t DecideRoomCap(uint64_t estimate, const SolveParams& params) {
      if (estimate <= kRoomCapCandidateLimit) return 0;
      return params.max_candidates_per_lesson > 0
        ? params.max_candidates_per_lesson
        : kDefaultMaxCandidatesPerLesson;
    }

    // One fresh decision var per candidate.
    std::vector<sat::BoolVar> DecisionVars(const CandidateSet& cs,
      sat::CpModelBuilder& cp) {
      std::vector<sat::BoolVar> x;
      x.reserve(cs.all.size());
      for (size_t i = 0; i < cs.all.size(); ++i) x.push_back(cp.NewBoolVar());
      return x;
    }

    // Small enough to presolve the full model: build the soft objective and
    // optimize directly — the exact, unchanged behavior of every small
    // instance.
    SolveResult OptimizeDirectly(const SchoolModel& m, const ModelIndex& ix,
      const RuleResolver& rules, const CandidateSet& cs,
      const SolveParams& params, const RunOptions& opts,
      std::atomic<bool>* cancel, const ProgressFn& on_improved,
      const HeartbeatFn& on_progress, const std::function<double()>& elapsed,
      const StageFn& stage) {
      trace::Line("solve: %zu candidates <= %zu limit -> direct optimize",
        cs.all.size(), kFullSolveCandidateLimit);
      stage(SolveStage::kDirect, "optimizing the full model", cs.all.size());
      sat::CpModelBuilder cp;
      std::vector<sat::BoolVar> x = DecisionVars(cs, cp);
      AddHardConstraints(m, ix, cs, x, cp);
      if (!params.disable_warm_start) AddPlacementHints(m, ix, cs, x, cp);
      AddSoftObjective(m, ix, cs, x, cp, rules);
      SolveResult r = RunSearch(cp, m, cs, x, opts, cancel, on_improved,
        on_progress, elapsed);
      trace::Line("solve: DONE status=%d penalty=%ld time=%.1fs",
        static_cast<int>(r.status), r.objective, r.wall_time_seconds);
      return r;
    }

    // Too large for the full objective (CP-SAT never finishes presolve):
    // CONSTRUCT a feasible schedule from the hard constraints only (no
    // objective encoding), then polish it with LNS in whatever time remains.
    // Turns "no solution" into "a valid schedule".
    SolveResult ConstructAndPolish(const SchoolModel& m, const ModelIndex& ix,
      const CandidateSet& cs, const SolveParams& params,
      const RunOptions& opts, std::atomic<bool>* cancel,
      const ProgressFn& on_improved, const HeartbeatFn& on_progress,
      const StageSink& on_stage, const std::function<double()>& elapsed,
      const StageFn& stage) {
      trace::Line("solve: %zu candidates > %zu limit -> construct + LNS polish",
        cs.all.size(), kFullSolveCandidateLimit);
      stage(SolveStage::kConstruct, "searching for any feasible schedule",
        cs.all.size());
      sat::CpModelBuilder cp;
      std::vector<sat::BoolVar> x = DecisionVars(cs, cp);
      AddHardConstraints(m, ix, cs, x, cp);
      if (!params.disable_warm_start) AddPlacementHints(m, ix, cs, x, cp);
      trace::Line("construct: searching for a feasible schedule...");
      SolveResult r = RunSearch(cp, m, cs, x, opts, cancel,
        /*on_improved=*/nullptr, on_progress, elapsed);
      if (r.best.lessons.empty()) {  // could not even construct
        trace::Line("construct: FAILED status=%d (%s) time=%.1fs",
          static_cast<int>(r.status), r.message.c_str(),
          r.wall_time_seconds);
        return r;
      }
      trace::Line("construct: feasible schedule found, penalty=%ld at %.1fs",
        ComputeScore(m, r.best).total_penalty, elapsed());

      // Each LNS pass re-optimizes a few divisions against the rest held
      // fixed, so the objective is only ever encoded over a small sub-model.
      if (!params.disable_lns) {
        r.best = PolishByLns(m, r.best, params, cancel, on_improved, elapsed,
          on_stage);
      }
      else {
        trace::Line("lns: disabled by config -> returning the construct schedule");
      }
      r.status = (cancel != nullptr && cancel->load())
        ? SolveStatusCode::kCancelled
        : SolveStatusCode::kFeasible;
      r.objective = ComputeScore(m, r.best).total_penalty;
      r.wall_time_seconds = elapsed();
      trace::Line("solve: DONE status=%d penalty=%ld time=%.1fs",
        static_cast<int>(r.status), r.objective, r.wall_time_seconds);
      return r;
    }

  }  // namespace

  SolveResult SolveSchedule(const SchoolModel& m, const SolveParams& params,
    std::atomic<bool>* cancel,
    const ProgressFn& on_improved,
    const HeartbeatFn& on_progress,
    const StageSink& on_stage) {
    const auto t0 = std::chrono::steady_clock::now();
    auto elapsed = [&t0] {
      return std::chrono::duration<double>(std::chrono::steady_clock::now() - t0)
        .count();
      };
    auto stage = [&] (SolveStage s, std::string detail,
      uint64_t candidates = 0) {
      if (!on_stage) return;
      SolveProgressInfo info;
      info.stage = s;
      info.detail = std::move(detail);
      info.candidates = candidates;
      info.stage_elapsed_seconds = elapsed();
      on_stage(info);
      };

    ModelIndex ix = ModelIndex::Build(m);
    // Rule modes resolved once for the whole solve (score/rules.h): the
    // preflight refuses config mistakes, the objective picks each term's
    // exit (hard/soft/off) through the same resolver.
    const RuleResolver rules = RuleResolver::Build(m, ix, params.rule_config);
    // Preflight: refuse impossible inputs with named culprits before any
    // CP-SAT work (validate/preflight.h — pigeonholes, room pools, locked
    // collisions, stream cap). Advisory findings are the service's business.
    const PreflightReport preflight = params.skip_preflight
      ? PreflightReport{}
      : RunPreflight(m, ix, params.max_streams_per_division, rules);
    if (!preflight.hard.empty()) {
      SolveResult r = RefusedByPreflight(preflight, elapsed());
      trace::Line("solve: REFUSED by preflight: %s", r.message.c_str());
      stage(SolveStage::kPreflight, "refused: " + r.message, 0);
      return r;
    }
    stage(SolveStage::kPreflight,
      preflight.advisory.empty()
        ? "ok"
        : std::to_string(preflight.advisory.size()) + " advisory finding(s): " +
          preflight.advisory.front().message,
      0);

    // Capped rooms are still real eligible rooms, so any solution stays
    // valid, and LNS sub-models (small) keep the full room choice.
    const uint64_t estimate =
      EstimateCandidateCount(m, ix, params.unknown_capacity_policy);
    const uint32_t room_cap = DecideRoomCap(estimate, params);
    auto cand_result =
      BuildCandidates(m, ix, params.unknown_capacity_policy, room_cap,
        &rules);
    if (std::holds_alternative<std::string>(cand_result)) {
      trace::Line("solve: candidate generation failed: %s",
        std::get<std::string>(cand_result).c_str());
      SolveResult r;
      r.status = SolveStatusCode::kInfeasible;
      r.message = std::get<std::string>(cand_result);
      r.wall_time_seconds = elapsed();
      return r;
    }
    const CandidateSet& cs = std::get<CandidateSet>(cand_result);
    if (room_cap > 0) {
      trace::Line("solve: ~%llu estimated candidates > %zu limit -> "
        "room cap %u/lesson -> %zu built",
        static_cast<unsigned long long>(estimate), kRoomCapCandidateLimit,
        room_cap, cs.all.size());
    }
    trace::Line("solve: divisions=%zu teachers=%zu rooms=%zu lessons=%zu "
      "candidates=%zu  time_budget=%.0fs workers=%d",
      m.divisions.size(), m.teachers.size(), m.rooms.size(),
      m.lessons.size(), cs.all.size(), params.max_time_seconds,
      params.num_workers);
    stage(SolveStage::kCandidates,
      room_cap > 0 ? "room cap " + std::to_string(room_cap) + "/lesson" : "",
      cs.all.size());

    const RunOptions opts{ params.max_time_seconds, params.num_workers,
                          params.random_seed };

    if (cs.all.size() <= kFullSolveCandidateLimit) {
      return OptimizeDirectly(m, ix, rules, cs, params, opts, cancel,
        on_improved, on_progress, elapsed, stage);
    }
    return ConstructAndPolish(m, ix, cs, params, opts, cancel, on_improved,
      on_progress, on_stage, elapsed, stage);
  }

}  // namespace arrango
