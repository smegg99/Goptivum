// src/score/scorer_internal.h

#pragma once

#include <map>
#include <optional>
#include <string>
#include <vector>

#include "model/atoms.h"
#include "model/daily_load.h"
#include "model/index.h"
#include "model/model.h"
#include "model/streams.h"
#include "score/scorer.h"

namespace arrango {
namespace scoring {

// One placed lesson resolved to indices, kept for fast repeated scans.
struct Placed {
  int lesson_idx;
  int day_idx;
  uint32_t start;
  uint32_t duration;
  int room_idx;  // -1 = none
  Placement placement;
};

struct CatAcc {
  int64_t penalty{};
  uint32_t count{};
};
// std::map keeps breakdown order deterministic.
using CatMap = std::map<std::string, CatAcc>;

// Accumulates a penalty into a category (no-op for zero).
void AddPenalty(CatMap& cats, const char* category, int64_t penalty,
                uint32_t count = 1);
// Records a count with ZERO penalty — used by hard/off rule modes so the
// metrics keep seeing violations the objective no longer prices.
void AddCount(CatMap& cats, const char* category, uint32_t count);

// A user-overridden soft weight, or the built-in base when not overridden.
inline int64_t WeightOr(const ResolvedRule& rule, int64_t base) {
  return rule.weight > 0 ? rule.weight : base;
}
// Flattens a category map into report items, adding each into *total.
std::vector<PenaltyItem> ToItems(const CatMap& cats, int64_t* total);

// Computes the full score breakdown for a schedule. Borrows everything
// model-derived from a ScoringContext (which must outlive it), so scoring
// many snapshots of one model pays the derivation cost once. Its methods are
// defined across scorer.cc (core), scorer_students.cc (stream + per-lesson
// terms), scorer_teachers.cc, scorer_info.cc (hygiene detectors), and
// scorer_aggregate.cc — one logical block per file.
class Scorer {
 public:
  Scorer(const ScoringContext& context, const ScheduleSnapshot& s);
  ScoreReport Run();

 private:
  // Empty periods strictly between the first and last occupied one, with the
  // multi-period-window escalation counted separately.
  struct GapInfo {
    int64_t count{};
    int first_gap{ -1 };
    int64_t window_pairs{};
  };

  uint32_t StreamPriority(const StudentStream& stream) const;
  std::vector<std::vector<bool>> StreamOccupancy(size_t s_i) const;
  static GapInfo Gaps(const std::vector<bool>& day_occ);
  LessonRef RefOf(const Placed* p) const;
  std::vector<LessonRef> StreamDayRefs(size_t s_i, int day_idx) const;
  void AddIssue(const char* category, std::string entity, Id entity_id,
                bool teacher, int day_idx, uint32_t period, uint32_t count,
                int64_t penalty, std::vector<LessonRef> lessons = {},
                bool config_hard = false);
  // The rule mode/weight for a category's rule at a division scope, with
  // the division's year resolved. kNoId subject = no subject dimension.
  ResolvedRule DivisionRule(const char* rule, int division_idx,
                            Id subject_id = kNoId) const;
  // One counted violation under its rule mode: soft -> penalty + warning
  // issue; hard -> count + config-hard issue (ERROR tier, zero penalty);
  // off -> count only (metrics keep observing, nobody is warned).
  void ApplyRule(const ResolvedRule& rule, CatMap& cats, const char* category,
                 int64_t soft_penalty, uint32_t count, std::string entity,
                 Id entity_id, bool teacher, int day_idx, uint32_t period,
                 std::vector<LessonRef> lessons = {});

  MetricInputs DivisionMetricInputs(size_t c, int64_t active_days,
                                    bool load_rules, bool max_prefs) const;
  MetricInputs TeacherMetricInputs(size_t t, int64_t active_days) const;
  // ResolveDailyLoad scans all lessons per atom; every stream of a division
  // shares one rule, so resolve once per division.
  const EffectiveDailyLoad& DailyLoadOf(int division_idx);

  void ScoreStreams();
  void ScoreStreamGaps(size_t s_i, const std::vector<std::vector<bool>>& occ,
                       CatMap& cats, uint32_t priority);
  void ScoreLateLessons(size_t s_i, CatMap& cats, uint32_t priority);
  void ScoreSubjectRuns(size_t s_i, CatMap& cats, uint32_t priority);
  void ScoreMaxLessons(size_t s_i, const std::vector<std::vector<bool>>& occ,
                       CatMap& cats, uint32_t priority);
  void ScoreDailyLoad(size_t s_i, const std::vector<std::vector<bool>>& occ,
                      CatMap& cats, uint32_t priority);
  void ScoreTeachers();
  void ScoreLessonCosts();
  void ScoreInfo();
  ScoreReport Aggregate();

  const SchoolModel& m_;
  const Weights& w_;
  const ModelIndex& ix_;
  const AtomSet& atoms_;
  const std::vector<StudentStream>& streams_;
  const RatingConfig& rating_;
  const RuleResolver& rules_;
  std::vector<Placed> placed_;
  std::vector<std::vector<const Placed*>> stream_members_;
  std::vector<CatMap> division_cats_;
  std::vector<CatMap> teacher_cats_;
  std::vector<SoftIssue> issues_;
  std::vector<SoftIssue> info_issues_;  // hygiene findings (INFO tier)
  // Metric-only counts (zero penalty, so they bypass the CatMaps): active
  // teacher-days holding exactly one lesson instance. Filled by
  // ScoreTeachers, consumed by the rating aggregation.
  std::vector<uint32_t> single_days_of_teacher_;
  // Rating denominators/counters (rates in score/metrics.h). "Periods" are
  // stream-member x period units — the exact universe the late penalties
  // count in, so the lateness rate divides like with like.
  std::vector<int64_t> student_periods_of_division_;
  std::vector<int64_t> late_student_periods_of_division_;
  std::vector<int64_t> periods_of_teacher_;
  std::vector<int64_t> late_periods_of_teacher_;
  std::vector<uint32_t> active_days_of_teacher_;
  std::vector<std::optional<EffectiveDailyLoad>> daily_load_cache_;
};

}  // namespace scoring
}  // namespace arrango
