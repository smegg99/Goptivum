// src/service/service.cc

#include "service/service.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <mutex>
#include <optional>
#include <thread>
#include <utility>

#include "demo/demo_data.h"
#include "score/scorer.h"
#include "service/convert.h"
#include "solve/explain.h"
#include "solve/solver.h"
#include "validate/validator.h"

namespace arrango {
  namespace {

    v1::SolveStatus StatusToProto(SolveStatusCode s) {
      switch (s) {
      case SolveStatusCode::kOptimal:
        return v1::SOLVE_STATUS_OPTIMAL;
      case SolveStatusCode::kFeasible:
        return v1::SOLVE_STATUS_FEASIBLE;
      case SolveStatusCode::kInfeasible:
        return v1::SOLVE_STATUS_INFEASIBLE;
      case SolveStatusCode::kCancelled:
        return v1::SOLVE_STATUS_CANCELLED;
      case SolveStatusCode::kTimeout:
        return v1::SOLVE_STATUS_TIMEOUT;
      case SolveStatusCode::kError:
        return v1::SOLVE_STATUS_ERROR;
      }
      return v1::SOLVE_STATUS_UNSPECIFIED;
    }

    DemoPreset PresetFromProto(v1::DemoPreset p) {
      return p == v1::DEMO_PRESET_MEGA ? DemoPreset::kMega
                                       : DemoPreset::kProduction;
    }

    // Fills snapshot + fresh validation and score into one update; returns
    // whether the snapshot passed hard validation (the audit fallback keeps
    // the newest solution this returned true for).
    bool FillSolution(const SchoolModel& model, const RatingConfig& rating,
      const RuleConfig& request_rules, const SolveResult& result,
      v1::SolveUpdate* update) {
      SnapshotToProto(result.best, update->mutable_snapshot());
      update->set_objective(result.objective);
      update->set_best_bound(result.best_bound);
      update->set_solutions_found(result.solutions_found);
      update->set_wall_time_seconds(result.wall_time_seconds);
      const ValidationReport validation = Validate(model, result.best);
      const ScoreReport score =
        ComputeScore(model, result.best, rating, request_rules);
      ValidationToProto(validation, update->mutable_validation());
      ScoreToProto(score, update->mutable_score());
      VerdictToProto(ComposeVerdict(validation, score),
        update->mutable_verdict());
      return validation.valid;
    }

    // Resolves the runtime config: an explicit `config` wins; otherwise the
    // legacy `params` map onto it. Weights unset in the config default to the
    // model's — and config-provided weights are written INTO the model — so
    // the echoed snapshot always records the weights actually used.
    SolverConfig ResolveRequestConfig(const v1::SolveRequest& request,
      const RuleConfig& request_rules, SchoolModel& model) {
      SolverConfig config;
      if (request.has_config()) {
        config = ConfigFromProto(request.config());
      }
      else {
        config.total_time_seconds = request.params().max_time_seconds() > 0
          ? request.params().max_time_seconds()
          : config.total_time_seconds;
        config.num_workers = static_cast<int>(request.params().num_workers());
        config.random_seed = request.params().random_seed();
        config.ignore_previous_placements = request.params().from_scratch();
      }
      const bool weights_from_config =
        request.has_config() && !(config.weights == Weights{});
      if (weights_from_config) {
        model.weights = config.weights;  // config-provided weights drive the solve
      }
      else {
        config.weights = model.weights;  // echo the weights actually used
      }
      // Echo the effective rule layers: the model's philosophy first, then
      // the request's on top (matching the resolver's precedence order).
      config.rule_config.profile = request_rules.profile.empty()
        ? model.rule_config.profile : request_rules.profile;
      config.rule_config.overrides = model.rule_config.overrides;
      config.rule_config.overrides.insert(config.rule_config.overrides.end(),
        request_rules.overrides.begin(), request_rules.overrides.end());
      return config;
    }

    SolveParams ParamsFromConfig(const SolverConfig& config,
      const RuleConfig& request_rules) {
      SolveParams params;
      params.max_time_seconds = config.total_time_seconds;
      params.num_workers = config.num_workers;  // 0 = all cores
      params.random_seed = config.random_seed;
      params.unknown_capacity_policy = config.unknown_capacity_policy;
      params.disable_lns = config.disable_lns;
      params.lns_seconds_per_neighborhood = config.lns_seconds_per_neighborhood;
      params.max_candidates_per_lesson = config.max_candidates_per_lesson;
      params.max_streams_per_division = config.max_streams_per_division;
      params.disable_warm_start = config.disable_warm_start;
      params.rule_config = request_rules;
      return params;
    }

    // The previous timetable as the solve baseline. Valid only when EVERY
    // lesson carries a placement, the whole schedule is hard-valid, AND it
    // satisfies every rule the school set to HARD (gap windows are hard by
    // default, so imported schedules with 2h holes keep being rejected) —
    // the solver can never regress below the baseline, so it must be a
    // schedule the solver could legally produce.
    struct Baseline {
      bool valid{ false };
      ScheduleSnapshot snapshot;
      ScoreReport score;
    };

    Baseline ResolveBaseline(const SchoolModel& model,
      const RatingConfig& rating, const RuleConfig& request_rules) {
      Baseline baseline;
      if (model.lessons.empty()) return baseline;
      for (const auto& lesson : model.lessons) {
        if (!lesson.previous_placement) return baseline;
        baseline.snapshot.lessons.push_back(
          { lesson.id, *lesson.previous_placement });
      }
      if (!Validate(model, baseline.snapshot).valid) return baseline;
      baseline.score =
        ComputeScore(model, baseline.snapshot, rating, request_rules);
      for (const SoftIssue& issue : baseline.score.soft_issues) {
        if (issue.config_hard && issue.count > 0) return baseline;
      }
      baseline.valid = true;
      return baseline;
    }

    // Never end up worse than the valid previous timetable: when the search
    // lost (no solution, or a higher penalty), the baseline becomes the
    // result — unless the solve proved INFEASIBLE/ERROR, which must surface.
    void KeepBaselineIfBetter(const Baseline& baseline, SolveResult& result) {
      if (!baseline.valid) return;
      const bool search_won =
        (result.status == SolveStatusCode::kOptimal ||
          result.status == SolveStatusCode::kFeasible ||
          result.status == SolveStatusCode::kCancelled) &&
        !result.best.lessons.empty();
      if (search_won && result.objective <= baseline.score.total_penalty) {
        return;
      }
      if (result.status == SolveStatusCode::kInfeasible ||
        result.status == SolveStatusCode::kError) {
        return;
      }
      result.best = baseline.snapshot;
      result.objective = baseline.score.total_penalty;
      if (result.status == SolveStatusCode::kTimeout) {
        result.status = SolveStatusCode::kFeasible;
      }
      result.message = "kept previous timetable (search found nothing better)";
    }

    // Polls gRPC client cancellation into an atomic the solver watches.
    // Stop() (or destruction) ends the poll thread.
    class CancelWatcher {
    public:
      explicit CancelWatcher(grpc::ServerContext* context)
        : thread_([this, context] {
            while (!done_.load()) {
              if (context->IsCancelled()) {
                std::fprintf(stderr, "solve: client cancellation detected\n");
                cancelled_.store(true);
                return;
              }
              std::this_thread::sleep_for(std::chrono::milliseconds(50));
            }
          }) {
      }
      ~CancelWatcher() { Stop(); }
      void Stop() {
        done_.store(true);
        if (thread_.joinable()) thread_.join();
      }
      std::atomic<bool>& flag() { return cancelled_; }

    private:
      std::atomic<bool> cancelled_{ false };
      std::atomic<bool> done_{ false };
      std::thread thread_;
    };

    // Infeasibility explainer: turns "proven infeasible" into a named core of
    // clashing inputs on the DONE update. Runs on its own budget, after the
    // verdict is known.
    void AttachInfeasibleCore(const SchoolModel& model,
      const RuleConfig& request_rules, const SolveParams& params,
      const SolverConfig& config, const std::string& base_message,
      v1::SolveUpdate& final_update) {
      const double budget = config.explain_budget_seconds > 0
        ? config.explain_budget_seconds
        : std::clamp(config.total_time_seconds * 0.25, 5.0, 30.0);
      const ModelIndex explain_ix = ModelIndex::Build(model);
      std::optional<InfeasibleCore> explained = ExplainInfeasibility(
        model, explain_ix, request_rules, params, budget);
      if (explained) {
        CoreToProto(*explained, final_update.mutable_infeasible_core());
        final_update.set_message(base_message + " — " + explained->message);
      }
    }

    // Timeout with nothing found: cheap heuristic suspects, clearly labeled
    // as guesses rather than a proof.
    void AttachTimeoutHints(const SchoolModel& model,
      v1::SolveUpdate& final_update) {
      InfeasibleCore hints_only;
      hints_only.hints = TimeoutHints(model, ModelIndex::Build(model));
      if (hints_only.hints.empty()) return;
      hints_only.message = "no schedule found in time; usual suspects "
        "(heuristic, not a proof)";
      CoreToProto(hints_only, final_update.mutable_infeasible_core());
    }

  }  // namespace

  AuditVerdict AuditResult(const SchoolModel& model,
    const RuleConfig& request_rules, const SolveResult& result) {
    AuditVerdict verdict;
    if (result.best.lessons.empty()) return verdict;  // nothing to audit
    const ValidationReport validation = Validate(model, result.best);
    if (!validation.valid) {
      verdict.ok = false;
      verdict.reason = "result fails validation: " +
        (validation.conflicts.empty() ? std::string("unknown conflict")
                                      : validation.conflicts.front().message);
      return verdict;
    }
    const int64_t rescored =
      ComputeScore(model, result.best, {}, request_rules).total_penalty;
    if (rescored != result.objective) {
      verdict.ok = false;
      verdict.reason = "reported objective " + std::to_string(result.objective) +
        " != independent rescore " + std::to_string(rescored);
    }
    return verdict;
  }

  grpc::Status SolverServiceImpl::GetDemoSchool(grpc::ServerContext*,
    const v1::DemoRequest* request,
    v1::SchoolModel* response) {
    ModelToProto(GenerateDemoSchool(PresetFromProto(request->preset()),
      request->seed()),
      response);
    return grpc::Status::OK;
  }

  grpc::Status SolverServiceImpl::Validate(grpc::ServerContext*,
    const v1::ValidateRequest* request,
    v1::ValidationReport* response) {
    ValidationToProto(arrango::Validate(ModelFromProto(request->model()),
      SnapshotFromProto(request->snapshot())),
      response);
    return grpc::Status::OK;
  }

  grpc::Status SolverServiceImpl::Score(grpc::ServerContext*,
    const v1::ScoreRequest* request,
    v1::ScoreReport* response) {
    ScoreToProto(ComputeScore(ModelFromProto(request->model()),
      SnapshotFromProto(request->snapshot()),
      RatingFromProto(request->reporting())),
      response);
    return grpc::Status::OK;
  }

  // Solve streams, in order: STARTED -> (baseline SOLUTION) -> PROGRESS
  // stage events + heartbeats + improved SOLUTIONs -> VALIDATE -> DONE.
  // Reads as five phases: resolve config, stream the baseline, run the
  // search with live callbacks, audit the result, emit DONE.
  grpc::Status SolverServiceImpl::Solve(
    grpc::ServerContext* context, const v1::SolveRequest* request,
    grpc::ServerWriter<v1::SolveUpdate>* writer) {
    SchoolModel model = ModelFromProto(request->model());

    // Rating (reporting) curves for every emitted score — independent of the
    // search weights the config resolves.
    const RatingConfig rating = RatingFromProto(request->reporting());
    const RuleConfig request_rules = RuleConfigFromProto(request->rule_config());
    SolverConfig config = ResolveRequestConfig(*request, request_rules, model);

    // Echo the resolved config on every update.
    auto attach_config = [&] (v1::SolveUpdate* u) {
      ConfigToProto(config, u->mutable_config());
      };

    // Only the full pipeline exists; a staged mode reports a clear error
    // instead of quietly running the wrong thing.
    if (config.mode != SolveMode::kFullPipeline) {
      v1::SolveUpdate done;
      done.set_phase(v1::SOLVE_PHASE_DONE);
      done.set_status(v1::SOLVE_STATUS_ERROR);
      done.set_message("solve mode not implemented yet "
        "(only the full pipeline runs)");
      attach_config(&done);
      writer->Write(done);
      return grpc::Status::OK;
    }

    if (config.ignore_previous_placements) {
      // Design freely: no hints, no never-worse baseline.
      for (LessonInstance& lesson : model.lessons) {
        lesson.previous_placement.reset();
      }
    }
    const SolveParams params = ParamsFromConfig(config, request_rules);

    std::mutex write_mutex;  // solution callbacks may come from solver threads
    {
      v1::SolveUpdate started;
      started.set_phase(v1::SOLVE_PHASE_STARTED);
      attach_config(&started);
      std::lock_guard<std::mutex> lock(write_mutex);
      writer->Write(started);
    }

    // A valid imported/prior timetable is streamed immediately and never
    // regressed below by the final result.
    const Baseline baseline = ResolveBaseline(model, rating, request_rules);
    if (baseline.valid) {
      SolveResult as_result;
      as_result.status = SolveStatusCode::kFeasible;
      as_result.best = baseline.snapshot;
      as_result.objective = baseline.score.total_penalty;
      v1::SolveUpdate update;
      update.set_phase(v1::SOLVE_PHASE_SOLUTION);
      update.set_status(v1::SOLVE_STATUS_FEASIBLE);
      update.set_message("baseline: previous timetable");
      FillSolution(model, rating, request_rules, as_result, &update);
      attach_config(&update);
      std::lock_guard<std::mutex> lock(write_mutex);
      writer->Write(update);
    }

    // Live stage events: every sink call becomes a PROGRESS update, and the
    // 1 Hz heartbeat repeats the latest stage so the stream never goes quiet
    // (a 50 s construct phase must visibly tick). `latest_progress` is
    // guarded by write_mutex like everything touching the writer.
    SolveProgressInfo latest_progress;
    auto on_stage = [&] (const SolveProgressInfo& info) {
      v1::SolveUpdate update;
      update.set_phase(v1::SOLVE_PHASE_PROGRESS);
      update.set_status(v1::SOLVE_STATUS_FEASIBLE);
      ProgressToProto(info, update.mutable_progress());
      attach_config(&update);
      std::lock_guard<std::mutex> lock(write_mutex);
      latest_progress = info;
      writer->Write(update);
      };

    // Newest hard-valid solution already streamed: the audit's fallback.
    std::optional<std::pair<ScheduleSnapshot, int64_t>> best_valid_streamed;
    if (baseline.valid) {
      best_valid_streamed = { baseline.snapshot, baseline.score.total_penalty };
    }

    CancelWatcher cancel_watcher(context);
    SolveResult result = SolveSchedule(
      model, params, &cancel_watcher.flag(),
      [&] (const SolveResult& improved) {
        v1::SolveUpdate update;
        update.set_phase(v1::SOLVE_PHASE_SOLUTION);
        update.set_status(v1::SOLVE_STATUS_FEASIBLE);
        const bool valid = FillSolution(model, rating, request_rules, improved, &update);
        attach_config(&update);
        std::lock_guard<std::mutex> lock(write_mutex);
        if (valid) best_valid_streamed = { improved.best, improved.objective };
        writer->Write(update);
      },
      [&] (const SolveResult& tick) {
        v1::SolveUpdate update;
        update.set_phase(v1::SOLVE_PHASE_PROGRESS);
        update.set_status(v1::SOLVE_STATUS_FEASIBLE);
        update.set_objective(tick.objective);
        update.set_best_bound(tick.best_bound);
        update.set_solutions_found(tick.solutions_found);
        update.set_wall_time_seconds(tick.wall_time_seconds);
        attach_config(&update);
        std::lock_guard<std::mutex> lock(write_mutex);
        ProgressToProto(latest_progress, update.mutable_progress());
        writer->Write(update);
      },
      on_stage);
    cancel_watcher.Stop();

    KeepBaselineIfBetter(baseline, result);

    {
      SolveProgressInfo validating;
      validating.stage = SolveStage::kValidate;
      validating.detail = "validating and scoring the final schedule";
      on_stage(validating);
    }

    // Cross-audit: never ship a result the independent validator/scorer
    // disagrees with. On failure fall back to the newest hard-valid streamed
    // solution and say so — a solver bug must be loud, not silent.
    if (!result.best.lessons.empty()) {
      const AuditVerdict audit = AuditResult(model, request_rules, result);
      if (!audit.ok) {
        if (best_valid_streamed) {
          result.best = best_valid_streamed->first;
          result.objective = best_valid_streamed->second;
        }
        else {
          result.best = {};
        }
        result.status = SolveStatusCode::kError;
        result.message = "internal audit failed: " + audit.reason;
      }
    }

    v1::SolveUpdate final_update;
    final_update.set_phase(v1::SOLVE_PHASE_DONE);
    final_update.set_status(StatusToProto(result.status));
    final_update.set_message(result.message);
    final_update.set_wall_time_seconds(result.wall_time_seconds);
    if (!result.best.lessons.empty()) {
      FillSolution(model, rating, request_rules, result, &final_update);
    }

    if (result.status == SolveStatusCode::kInfeasible &&
      !config.disable_infeasibility_explainer) {
      {
        v1::SolveUpdate explaining;
        explaining.set_phase(v1::SOLVE_PHASE_PROGRESS);
        SolveProgressInfo info;
        info.stage = SolveStage::kExplain;
        info.detail = "isolating the clashing inputs";
        ProgressToProto(info, explaining.mutable_progress());
        attach_config(&explaining);
        std::lock_guard<std::mutex> lock(write_mutex);
        writer->Write(explaining);
      }
      AttachInfeasibleCore(model, request_rules, params, config,
        result.message, final_update);
    }
    if (result.status == SolveStatusCode::kTimeout &&
      result.best.lessons.empty() &&
      !config.disable_infeasibility_explainer) {
      AttachTimeoutHints(model, final_update);
    }
    attach_config(&final_update);
    std::lock_guard<std::mutex> lock(write_mutex);
    writer->Write(final_update);
    return grpc::Status::OK;
  }

}  // namespace arrango
