// src/solve/cpsat_runner.cc

#include "solve/cpsat_runner.h"

#include <chrono>
#include <cstdlib>
#include <thread>

#include "ortools/sat/cp_model_solver.h"
#include "ortools/sat/model.h"
#include "ortools/sat/sat_parameters.pb.h"
#include "ortools/util/time_limit.h"

namespace arrango {
  namespace {

    namespace sat = operations_research::sat;

    // Reads the chosen candidate per lesson out of a CP-SAT solution.
    ScheduleSnapshot ExtractSnapshot(const SchoolModel& m, const CandidateSet& cs,
      const std::vector<sat::BoolVar>& x,
      const sat::CpSolverResponse& response) {
      ScheduleSnapshot snapshot;
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        for (int ci : cs.by_lesson[li]) {
          if (!sat::SolutionBooleanValue(response, x[ci])) continue;
          const Candidate& c = cs.all[ci];
          Placement p;
          p.day_id = m.days[c.day_idx].id;
          p.start_period = c.start;
          p.room_id = c.room_idx >= 0 ? m.rooms[c.room_idx].id : kNoId;
          snapshot.lessons.push_back({ m.lessons[li].id, p });
          break;
        }
      }
      return snapshot;
    }

  }  // namespace

  SolveResult RunSearch(sat::CpModelBuilder& cp, const SchoolModel& m,
    const CandidateSet& cs,
    const std::vector<sat::BoolVar>& x, const RunOptions& opts,
    std::atomic<bool>* cancel, const ProgressFn& on_improved,
    const HeartbeatFn& on_progress,
    const std::function<double()>& elapsed) {
    // Shared search telemetry for solution/bound callbacks + heartbeat.
    std::atomic<int64_t> live_objective{ 0 };
    std::atomic<int64_t> live_bound{ 0 };
    std::atomic<int> live_solutions{ 0 };

    sat::Model sat_model;
    sat::SatParameters sp;
    sp.set_max_time_in_seconds(opts.max_time_seconds);
    const int workers = opts.num_workers > 0
      ? opts.num_workers
      : static_cast<int>(std::thread::hardware_concurrency());
    sp.set_num_search_workers(std::max(workers, 1));
    sp.set_random_seed(static_cast<int32_t>(opts.random_seed));
    sp.set_log_search_progress(std::getenv("ARRANGO_SAT_LOG") != nullptr);
    // NOTE: repair_hint(true) looked attractive for completing the partial
    // candidate hints, but it crashes OR-Tools 9.15 on long solves (CHECK failed:
    // heuristics.fixed_search != nullptr in integer_search.cc). Plain hints stay;
    // the service-level baseline fallback already guarantees re-solves never
    // regress.
    sat_model.Add(sat::NewSatParameters(sp));
    // OR-Tools WRITES to the registered stop flag when workers coordinate
    // shutdown (e.g. at the time limit), so the caller's cancel flag must stay
    // separate: a bridge thread copies cancel -> stop, and status mapping reads
    // only the caller's flag.
    std::atomic<bool> stop{ false };
    sat_model.GetOrCreate<operations_research::TimeLimit>()
      ->RegisterExternalBooleanAsLimit(&stop);
    std::atomic<bool> bridge_done{ false };
    std::thread cancel_bridge;
    if (cancel != nullptr) {
      cancel_bridge = std::thread([&] {
        while (!bridge_done.load()) {
          if (cancel->load()) {
            stop.store(true);
            return;
          }
          std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
        });
    }
    sat_model.Add(sat::NewFeasibleSolutionObserver(
      [&] (const sat::CpSolverResponse& response) {
        live_objective.store(static_cast<int64_t>(response.objective_value()));
        live_solutions.fetch_add(1);
        if (!on_improved) return;
        SolveResult intermediate;
        intermediate.status = SolveStatusCode::kFeasible;
        intermediate.best = ExtractSnapshot(m, cs, x, response);
        intermediate.objective =
          static_cast<int64_t>(response.objective_value());
        intermediate.best_bound = live_bound.load();
        intermediate.solutions_found = live_solutions.load();
        intermediate.wall_time_seconds = elapsed();
        on_improved(intermediate);
      }));
    sat_model.Add(sat::NewBestBoundCallback(
      [&] (double bound) { live_bound.store(static_cast<int64_t>(bound)); }));

    // ~1 Hz heartbeat with objective/bound only (no snapshot extraction).
    std::atomic<bool> search_done{ false };
    std::thread heartbeat;
    if (on_progress) {
      heartbeat = std::thread([&] {
        while (!search_done.load()) {
          std::this_thread::sleep_for(std::chrono::milliseconds(1000));
          if (search_done.load()) return;
          SolveResult tick;
          tick.status = SolveStatusCode::kFeasible;
          tick.objective = live_objective.load();
          tick.best_bound = live_bound.load();
          tick.solutions_found = live_solutions.load();
          tick.wall_time_seconds = elapsed();
          on_progress(tick);
        }
        });
    }

    const sat::CpSolverResponse response =
      sat::SolveCpModel(cp.Build(), &sat_model);
    search_done.store(true);
    bridge_done.store(true);
    if (heartbeat.joinable()) heartbeat.join();
    if (cancel_bridge.joinable()) cancel_bridge.join();

    SolveResult result;
    result.wall_time_seconds = elapsed();
    const bool cancelled = cancel != nullptr && cancel->load();
    const bool has_solution = response.status() == sat::CpSolverStatus::OPTIMAL ||
      response.status() == sat::CpSolverStatus::FEASIBLE;
    if (has_solution) {
      result.best = ExtractSnapshot(m, cs, x, response);
      result.objective = static_cast<int64_t>(response.objective_value());
    }
    result.best_bound = static_cast<int64_t>(response.best_objective_bound());
    result.solutions_found = live_solutions.load();
    switch (response.status()) {
    case sat::CpSolverStatus::OPTIMAL:
      result.status = SolveStatusCode::kOptimal;
      break;
    case sat::CpSolverStatus::FEASIBLE:
      result.status =
        cancelled ? SolveStatusCode::kCancelled : SolveStatusCode::kFeasible;
      break;
    case sat::CpSolverStatus::INFEASIBLE:
      result.status = SolveStatusCode::kInfeasible;
      result.message = "model proven infeasible";
      break;
    case sat::CpSolverStatus::MODEL_INVALID:
      result.status = SolveStatusCode::kError;
      result.message = "CP-SAT model invalid";
      break;
    default:
      result.status =
        cancelled ? SolveStatusCode::kCancelled : SolveStatusCode::kTimeout;
      result.message = cancelled ? "cancelled before a feasible solution"
        : "no feasible solution within time limit";
      break;
    }
    return result;
  }

}  // namespace arrango
