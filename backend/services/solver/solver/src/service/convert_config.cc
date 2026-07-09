// src/service/convert_config.cc

#include "service/convert.h"

namespace arrango {
  namespace {

    Weights WeightsFromProto(const v1::Weights& w) {
      return Weights{ w.student_gap_base(),        w.teacher_gap_base(),
                     w.late_student_lesson_base(), w.late_teacher_finish_base(),
                     w.subject_split_base(),      w.block_break_base(),
                     w.room_change_base(),        w.stability_move_base(),
                     w.expected_bad_per_lesson(), w.late_threshold_period(),
                     w.gap_cap_per_day(),         w.gap_window_base(),
                     w.early_start_base() };
    }

    void WeightsToProto(const Weights& in, v1::Weights* w) {
      w->set_student_gap_base(in.student_gap_base);
      w->set_teacher_gap_base(in.teacher_gap_base);
      w->set_late_student_lesson_base(in.late_student_lesson_base);
      w->set_late_teacher_finish_base(in.late_teacher_finish_base);
      w->set_subject_split_base(in.subject_split_base);
      w->set_block_break_base(in.block_break_base);
      w->set_room_change_base(in.room_change_base);
      w->set_stability_move_base(in.stability_move_base);
      w->set_expected_bad_per_lesson(in.expected_bad_per_lesson);
      w->set_late_threshold_period(in.late_threshold_period);
      w->set_gap_cap_per_day(in.gap_cap_per_day);
      w->set_gap_window_base(in.gap_window_base);
      w->set_early_start_base(in.early_start_base);
    }

    bool WeightsAllZero(const v1::Weights& w) {
      return w.student_gap_base() == 0 && w.teacher_gap_base() == 0 &&
        w.late_student_lesson_base() == 0 &&
        w.late_teacher_finish_base() == 0 && w.subject_split_base() == 0 &&
        w.block_break_base() == 0 && w.room_change_base() == 0 &&
        w.stability_move_base() == 0 && w.expected_bad_per_lesson() == 0 &&
        w.late_threshold_period() == 0 && w.gap_cap_per_day() == 0 &&
        w.gap_window_base() == 0 && w.early_start_base() == 0;
    }

  }  // namespace

  SolverConfig ConfigFromProto(const v1::SolverConfig& in) {
    SolverConfig c;  // starts from built-in defaults
    switch (in.mode()) {
    case v1::SOLVE_MODE_VIABILITY_CHECK: c.mode = SolveMode::kViabilityCheck; break;
    case v1::SOLVE_MODE_CONSTRUCT_FAST: c.mode = SolveMode::kConstructFast; break;
    case v1::SOLVE_MODE_CONSTRUCT_CLEAN: c.mode = SolveMode::kConstructClean; break;
    case v1::SOLVE_MODE_ASSIGN_ROOMS: c.mode = SolveMode::kAssignRooms; break;
    case v1::SOLVE_MODE_REPAIR: c.mode = SolveMode::kRepair; break;
    case v1::SOLVE_MODE_POLISH: c.mode = SolveMode::kPolish; break;
    default: c.mode = SolveMode::kFullPipeline; break;  // UNSPECIFIED/FULL
    }
    if (in.total_time_seconds() > 0) c.total_time_seconds = in.total_time_seconds();
    c.phase_limits = { in.phase_limits().construct_fast_seconds(),
                      in.phase_limits().construct_clean_seconds(),
                      in.phase_limits().assign_rooms_seconds(),
                      in.phase_limits().repair_seconds(),
                      in.phase_limits().polish_seconds() };
    c.num_workers = static_cast<int>(in.num_workers());  // 0 = all cores
    if (in.random_seed() != 0) c.random_seed = in.random_seed();
    if (in.exact_room_threshold() != 0) c.exact_room_threshold = in.exact_room_threshold();
    c.max_candidates_per_lesson = in.max_candidates_per_lesson();
    c.max_streams_per_division = in.max_streams_per_division();
    c.rule_config = RuleConfigFromProto(in.rule_config());
    c.disable_infeasibility_explainer = in.disable_infeasibility_explainer();
    c.explain_budget_seconds = in.explain_budget_seconds();
    c.disable_warm_start = in.disable_warm_start();
    c.portfolio_seeds = in.portfolio_seeds();
    c.disable_lns = in.disable_lns();
    if (in.lns_iterations() != 0) c.lns_iterations = in.lns_iterations();
    if (in.lns_seconds_per_neighborhood() > 0) {
      c.lns_seconds_per_neighborhood = in.lns_seconds_per_neighborhood();
    }
    switch (in.pool_strategy()) {
    case v1::POOL_STRATEGY_EXACT_ONLY: c.pool_strategy = PoolStrategy::kExactOnly; break;
    case v1::POOL_STRATEGY_POOL_LARGE: c.pool_strategy = PoolStrategy::kPoolLarge; break;
    default: c.pool_strategy = PoolStrategy::kAuto; break;
    }
    switch (in.pool_safety()) {
    case v1::POOL_SAFETY_MATCHING_CUTS: c.pool_safety = PoolSafety::kMatchingCuts; break;
    case v1::POOL_SAFETY_REPORT_REPAIR: c.pool_safety = PoolSafety::kReportRepair; break;
    default: c.pool_safety = PoolSafety::kStrict; break;
    }
    switch (in.unknown_capacity_policy()) {
    case v1::UNKNOWN_CAPACITY_FORBID:
      c.unknown_capacity_policy = UnknownCapacityPolicy::kForbid; break;
    case v1::UNKNOWN_CAPACITY_ALLOW_PENALIZED:
      c.unknown_capacity_policy = UnknownCapacityPolicy::kAllowPenalized; break;
    default: c.unknown_capacity_policy = UnknownCapacityPolicy::kAllow; break;
    }
    c.unplaced_policy = in.unplaced_policy() == v1::UNPLACED_ALLOW_PENALIZED
      ? UnplacedPolicy::kAllowPenalized
      : UnplacedPolicy::kForbid;
    c.ignore_previous_placements = in.ignore_previous_placements();
    c.ignore_viability_block = in.ignore_viability_block();
    if (!WeightsAllZero(in.weights())) c.weights = WeightsFromProto(in.weights());
    if (in.unplaced_penalty() != 0) c.unplaced_penalty = in.unplaced_penalty();
    return c;
  }

  void ConfigToProto(const SolverConfig& in, v1::SolverConfig* out) {
    switch (in.mode) {
    case SolveMode::kViabilityCheck: out->set_mode(v1::SOLVE_MODE_VIABILITY_CHECK); break;
    case SolveMode::kConstructFast: out->set_mode(v1::SOLVE_MODE_CONSTRUCT_FAST); break;
    case SolveMode::kConstructClean: out->set_mode(v1::SOLVE_MODE_CONSTRUCT_CLEAN); break;
    case SolveMode::kAssignRooms: out->set_mode(v1::SOLVE_MODE_ASSIGN_ROOMS); break;
    case SolveMode::kRepair: out->set_mode(v1::SOLVE_MODE_REPAIR); break;
    case SolveMode::kPolish: out->set_mode(v1::SOLVE_MODE_POLISH); break;
    case SolveMode::kFullPipeline: out->set_mode(v1::SOLVE_MODE_FULL_PIPELINE); break;
    }
    out->set_total_time_seconds(in.total_time_seconds);
    v1::PhaseLimits* pl = out->mutable_phase_limits();
    pl->set_construct_fast_seconds(in.phase_limits.construct_fast_seconds);
    pl->set_construct_clean_seconds(in.phase_limits.construct_clean_seconds);
    pl->set_assign_rooms_seconds(in.phase_limits.assign_rooms_seconds);
    pl->set_repair_seconds(in.phase_limits.repair_seconds);
    pl->set_polish_seconds(in.phase_limits.polish_seconds);
    out->set_num_workers(static_cast<uint32_t>(in.num_workers));
    out->set_random_seed(in.random_seed);
    out->set_exact_room_threshold(in.exact_room_threshold);
    out->set_max_candidates_per_lesson(in.max_candidates_per_lesson);
    out->set_max_streams_per_division(in.max_streams_per_division);
    RuleConfigToProto(in.rule_config, out->mutable_rule_config());
    out->set_disable_infeasibility_explainer(
      in.disable_infeasibility_explainer);
    out->set_explain_budget_seconds(in.explain_budget_seconds);
    out->set_disable_warm_start(in.disable_warm_start);
    out->set_portfolio_seeds(in.portfolio_seeds);
    out->set_disable_lns(in.disable_lns);
    out->set_lns_iterations(in.lns_iterations);
    out->set_lns_seconds_per_neighborhood(in.lns_seconds_per_neighborhood);
    out->set_pool_strategy(
      in.pool_strategy == PoolStrategy::kExactOnly ? v1::POOL_STRATEGY_EXACT_ONLY
      : in.pool_strategy == PoolStrategy::kPoolLarge ? v1::POOL_STRATEGY_POOL_LARGE
      : v1::POOL_STRATEGY_AUTO);
    out->set_pool_safety(
      in.pool_safety == PoolSafety::kMatchingCuts ? v1::POOL_SAFETY_MATCHING_CUTS
      : in.pool_safety == PoolSafety::kReportRepair ? v1::POOL_SAFETY_REPORT_REPAIR
      : v1::POOL_SAFETY_STRICT);
    out->set_unknown_capacity_policy(
      in.unknown_capacity_policy == UnknownCapacityPolicy::kForbid
      ? v1::UNKNOWN_CAPACITY_FORBID
      : in.unknown_capacity_policy == UnknownCapacityPolicy::kAllowPenalized
      ? v1::UNKNOWN_CAPACITY_ALLOW_PENALIZED
      : v1::UNKNOWN_CAPACITY_ALLOW);
    out->set_unplaced_policy(in.unplaced_policy == UnplacedPolicy::kAllowPenalized
      ? v1::UNPLACED_ALLOW_PENALIZED
      : v1::UNPLACED_FORBID);
    out->set_ignore_previous_placements(in.ignore_previous_placements);
    out->set_ignore_viability_block(in.ignore_viability_block);
    WeightsToProto(in.weights, out->mutable_weights());
    out->set_unplaced_penalty(in.unplaced_penalty);
  }

  RuleConfig RuleConfigFromProto(const v1::RuleConfig& in) {
    RuleConfig config;
    config.profile = in.profile();
    for (const auto& o : in.overrides()) {
      RuleMode mode = RuleMode::kDefault;
      switch (o.mode()) {
      case v1::RULE_MODE_HARD: mode = RuleMode::kHard; break;
      case v1::RULE_MODE_SOFT: mode = RuleMode::kSoft; break;
      case v1::RULE_MODE_OFF: mode = RuleMode::kOff; break;
      default: break;
      }
      config.overrides.push_back({ o.rule(), mode, o.weight(), o.param(),
                                  o.year_id(), o.division_id(),
                                  o.subject_id(), o.teacher_id() });
    }
    return config;
  }

  void RuleConfigToProto(const RuleConfig& in, v1::RuleConfig* out) {
    out->set_profile(in.profile);
    for (const RuleOverride& o : in.overrides) {
      v1::RuleOverride* po = out->add_overrides();
      po->set_rule(o.rule);
      po->set_mode(o.mode == RuleMode::kHard ? v1::RULE_MODE_HARD
        : o.mode == RuleMode::kSoft ? v1::RULE_MODE_SOFT
        : o.mode == RuleMode::kOff ? v1::RULE_MODE_OFF
        : v1::RULE_MODE_DEFAULT);
      po->set_weight(o.weight);
      po->set_param(o.param);
      po->set_year_id(o.year_id);
      po->set_division_id(o.division_id);
      po->set_subject_id(o.subject_id);
      po->set_teacher_id(o.teacher_id);
    }
  }

  RatingConfig RatingFromProto(const v1::ReportingWeights& in) {
    RatingConfig config;
    for (const auto& [key, value] : in.half_life()) {
      config.half_life[key] = value;
    }
    for (const auto& [key, value] : in.metric_weight()) {
      config.weight[key] = value;
    }
    if (in.students_share() > 0) config.students_share = in.students_share();
    return config;
  }

}  // namespace arrango
