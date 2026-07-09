// src/score/scorer.h

#pragma once

#include <string>
#include <vector>

#include "model/atoms.h"
#include "model/index.h"
#include "model/locus.h"
#include "model/model.h"
#include "model/streams.h"
#include "score/metrics.h"
#include "score/rules.h"

namespace arrango {

  struct PenaltyItem {
    std::string category;
    int64_t penalty{};
    uint32_t count{};
  };

  // One located soft violation (a gap run, a day of late lessons, ...).
  struct SoftIssue {
    std::string category;
    std::string entity;  // display label, kept for compatibility
    Id entity_id{};
    bool teacher{ false };
    Id day_id{ kNoId };
    uint32_t period{};   // first affected 0-based period
    uint32_t count{ 1 };
    int64_t penalty{};
    // True when this violates a rule the school set to HARD: it counts as a
    // config-hard ERROR in the verdict, never as a warning, and carries no
    // penalty (the objective has no term for hard rules).
    bool config_hard{ false };
    // Structured localization (typed entities, lesson blocks, time spans).
    IssueLocus locus;
  };

  struct EntityScore {
    Id entity_id{};
    std::string name;
    double quality{};  // 0..100
    int64_t penalty{};
    std::vector<PenaltyItem> items;
  };

  struct ScoreReport {
    // Absolute metric-based qualities: 100 == pristine, comparable across
    // schools and configs (see score/metrics.h). total_penalty stays the
    // raw SEARCH objective — a different animal, kept for debugging.
    double overall_quality{};
    double all_students_quality{};
    double all_teachers_quality{};
    int64_t total_penalty{};
    std::vector<EntityScore> division_scores;
    std::vector<EntityScore> year_scores;
    std::vector<EntityScore> teacher_scores;
    std::vector<PenaltyItem> global_items;
    // Located issues, sorted by penalty (largest first).
    std::vector<SoftIssue> soft_issues;
    // School-level rates behind the composites (per-entity composites use
    // per-entity rates of the same metrics).
    std::vector<MetricScore> metric_scores;
    // Hygiene findings (INFO tier): reported, never scored, never blocking.
    std::vector<SoftIssue> info_issues;
  };

  // Model-derived lookups the scorer needs (index, atoms, streams). Building
  // them costs more than scoring a snapshot, so callers that score MANY
  // snapshots of one unchanged model (the LNS accept loop) build the context
  // once. The referenced model must outlive the context.
  struct ScoringContext {
    static ScoringContext Build(const SchoolModel& model,
      RatingConfig rating = {}, const RuleConfig& request_rules = {});

    const SchoolModel* model{};
    ModelIndex index;
    AtomSet atoms;
    std::vector<StudentStream> streams;
    Weights weights;      // model weights with defaults applied (SEARCH)
    RatingConfig rating;  // reporting curves/weights (RATING) — independent
    RuleResolver rules;   // hard/soft/off per rule and entity
  };

  // Pure, CP-SAT-free scorer. Penalties share semantics with the CP-SAT
  // objective through score/penalty_defs.h (drift-guarded); the quality
  // fields come from the absolute metric rating in score/metrics.h.
  ScoreReport ComputeScore(const SchoolModel& model,
    const ScheduleSnapshot& snapshot);
  ScoreReport ComputeScore(const SchoolModel& model,
    const ScheduleSnapshot& snapshot, const RatingConfig& rating);
  ScoreReport ComputeScore(const SchoolModel& model,
    const ScheduleSnapshot& snapshot, const RatingConfig& rating,
    const RuleConfig& request_rules);
  // Same result; reuses the prebuilt context instead of deriving it again.
  ScoreReport ComputeScore(const ScoringContext& context,
    const ScheduleSnapshot& snapshot);

}  // namespace arrango
