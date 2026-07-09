// src/score/scorer.cc

#include "score/scorer_internal.h"

#include <algorithm>

#include "score/penalty_defs.h"

namespace arrango {
namespace scoring {

void AddPenalty(CatMap& cats, const char* category, int64_t penalty,
                uint32_t count) {
  if (penalty == 0) return;
  cats[category].penalty += penalty;
  cats[category].count += count;
}

void AddCount(CatMap& cats, const char* category, uint32_t count) {
  if (count == 0) return;
  cats[category].count += count;
}

std::vector<PenaltyItem> ToItems(const CatMap& cats, int64_t* total) {
  std::vector<PenaltyItem> items;
  for (const auto& [cat, acc] : cats) {
    items.push_back({ cat, acc.penalty, acc.count });
    *total += acc.penalty;
  }
  return items;
}

Scorer::Scorer(const ScoringContext& context, const ScheduleSnapshot& s)
    : m_(*context.model),
      w_(context.weights),
      ix_(context.index),
      atoms_(context.atoms),
      streams_(context.streams),
      rating_(context.rating),
      rules_(context.rules) {
  for (const ScheduledLesson& sl : s.lessons) {
    int li = ix_.LessonIdx(sl.lesson_id);
    if (li < 0) continue;  // validator's business
    int di = ix_.DayIdx(sl.placement.day_id);
    if (di < 0) continue;
    const LessonInstance& l = m_.lessons[li];
    if (sl.placement.start_period + l.duration > m_.days[di].period_count) {
      continue;
    }
    placed_.push_back({ li, di, sl.placement.start_period, l.duration,
                       ix_.RoomIdx(sl.placement.room_id), sl.placement });
  }
  // Stream i is atom i (see BuildStudentStreams), so a lesson's atom list IS
  // its stream list — invert it directly instead of probing every stream.
  stream_members_.resize(streams_.size());
  for (const Placed& p : placed_) {
    for (int a : atoms_.of_lesson[p.lesson_idx]) {
      stream_members_[a].push_back(&p);
    }
  }
  division_cats_.resize(m_.divisions.size());
  teacher_cats_.resize(m_.teachers.size());
  daily_load_cache_.resize(m_.divisions.size());
  student_periods_of_division_.assign(m_.divisions.size(), 0);
  late_student_periods_of_division_.assign(m_.divisions.size(), 0);
  periods_of_teacher_.assign(m_.teachers.size(), 0);
  late_periods_of_teacher_.assign(m_.teachers.size(), 0);
  active_days_of_teacher_.assign(m_.teachers.size(), 0);
}

ScoreReport Scorer::Run() {
  ScoreStreams();
  ScoreTeachers();
  ScoreLessonCosts();
  ScoreInfo();
  return Aggregate();
}

ResolvedRule Scorer::DivisionRule(const char* rule, int division_idx,
                                  Id subject_id) const {
  return rules_.For(rule, m_.divisions[division_idx].year_id,
                    m_.divisions[division_idx].id, subject_id, kNoId);
}

void Scorer::ApplyRule(const ResolvedRule& rule, CatMap& cats,
                       const char* category, int64_t soft_penalty,
                       uint32_t count, std::string entity, Id entity_id,
                       bool teacher, int day_idx, uint32_t period,
                       std::vector<LessonRef> lessons) {
  switch (rule.mode) {
  case RuleMode::kSoft:
  case RuleMode::kDefault:  // resolver never yields kDefault; treat as soft
    AddPenalty(cats, category, soft_penalty, count);
    AddIssue(category, std::move(entity), entity_id, teacher, day_idx,
             period, count, soft_penalty, std::move(lessons));
    break;
  case RuleMode::kHard:
    AddCount(cats, category, count);
    AddIssue(category, std::move(entity), entity_id, teacher, day_idx,
             period, count, /*penalty=*/0, std::move(lessons),
             /*config_hard=*/true);
    break;
  case RuleMode::kOff:
    AddCount(cats, category, count);
    break;
  }
}

const EffectiveDailyLoad& Scorer::DailyLoadOf(int division_idx) {
  std::optional<EffectiveDailyLoad>& slot = daily_load_cache_[division_idx];
  if (!slot) slot = ResolveDailyLoad(m_, ix_, atoms_, division_idx);
  return *slot;
}

uint32_t Scorer::StreamPriority(const StudentStream& stream) const {
  return ix_.YearPriority(
      ix_.YearIdx(m_.divisions[stream.division_idx].year_id));
}

// days x periods occupancy for one stream, external blocks included.
std::vector<std::vector<bool>> Scorer::StreamOccupancy(size_t s_i) const {
  std::vector<std::vector<bool>> occ(m_.days.size());
  for (size_t d = 0; d < m_.days.size(); ++d) {
    occ[d].assign(m_.days[d].period_count, false);
  }
  for (const Placed* p : stream_members_[s_i]) {
    for (uint32_t q = p->start; q < p->start + p->duration; ++q) {
      occ[p->day_idx][q] = true;
    }
  }
  const StudentStream& st = streams_[s_i];
  for (const ExternalBlock& blk : m_.external_blocks) {
    bool hits = false;
    if (blk.target == BlockTarget::kDivision) {
      hits = ix_.DivisionIdx(blk.target_id) == st.division_idx;
    }
    else if (blk.target == BlockTarget::kGroup) {
      // A group block occupies exactly the atoms the group covers.
      int g = ix_.GroupIdx(blk.target_id);
      hits = g >= 0 &&
             std::binary_search(atoms_.of_group[g].begin(),
                                atoms_.of_group[g].end(), st.atom_idx);
    }
    if (!hits) continue;
    int d = ix_.DayIdx(blk.day_id);
    if (d < 0) continue;
    for (uint32_t q = blk.start_period;
         q < blk.start_period + blk.duration && q < m_.days[d].period_count;
         ++q) {
      occ[d][q] = true;
    }
  }
  return occ;
}

Scorer::GapInfo Scorer::Gaps(const std::vector<bool>& day_occ) {
  int first = -1, last = -1;
  for (size_t p = 0; p < day_occ.size(); ++p) {
    if (day_occ[p]) {
      if (first < 0) first = static_cast<int>(p);
      last = static_cast<int>(p);
    }
  }
  GapInfo info;
  for (int p = first + 1; p < last; ++p) {
    if (first >= 0 && !day_occ[p]) {
      if (info.first_gap < 0) info.first_gap = p;
      ++info.count;
      if (p > first + 1 && !day_occ[p - 1]) ++info.window_pairs;
    }
  }
  return info;
}

// Structured lesson ref from a placed lesson.
LessonRef Scorer::RefOf(const Placed* p) const {
  const LessonInstance& l = m_.lessons[p->lesson_idx];
  return { l.id, m_.days[p->day_idx].id, p->start, p->duration,
          p->room_idx >= 0 ? m_.rooms[p->room_idx].id : kNoId };
}

// Lesson refs for one stream's placed lessons on a given day.
std::vector<LessonRef> Scorer::StreamDayRefs(size_t s_i, int day_idx) const {
  std::vector<LessonRef> refs;
  for (const Placed* p : stream_members_[s_i]) {
    if (p->day_idx == day_idx) refs.push_back(RefOf(p));
  }
  return refs;
}

void Scorer::AddIssue(const char* category, std::string entity, Id entity_id,
                      bool teacher, int day_idx, uint32_t period, uint32_t count,
                      int64_t penalty, std::vector<LessonRef> lessons,
                      bool config_hard) {
  // Config-hard violations carry no penalty (the objective has no term for
  // hard rules) but must still be reported — they are ERROR-tier.
  if (penalty == 0 && !(config_hard && count > 0)) return;
  SoftIssue issue{ category,
                  std::move(entity),
                  entity_id,
                  teacher,
                  day_idx >= 0 ? m_.days[day_idx].id : kNoId,
                  period,
                  count,
                  penalty,
                  config_hard,
                  {} };
  issue.locus.entities.push_back(
      { teacher ? EntityKind::kTeacher : EntityKind::kDivision, entity_id });
  if (day_idx >= 0) {
    issue.locus.spans.push_back(
        { m_.days[day_idx].id, period, std::max<uint32_t>(count, 1u) });
  }
  issue.locus.lessons = std::move(lessons);
  issues_.push_back(std::move(issue));
}

}  // namespace scoring

// Everything here derives purely from the model, so one context can score
// any number of snapshots. The caller keeps `model` alive for its lifetime.
ScoringContext ScoringContext::Build(const SchoolModel& model,
                                     RatingConfig rating,
                                     const RuleConfig& request_rules) {
  ScoringContext context;
  context.model = &model;
  context.index = ModelIndex::Build(model);
  context.atoms = BuildAtoms(model, context.index);
  context.streams = BuildStudentStreams(model, context.index, context.atoms);
  context.weights = WithDefaults(model.weights);
  context.rating = std::move(rating);
  context.rules =
      RuleResolver::Build(model, context.index, request_rules);
  return context;
}

ScoreReport ComputeScore(const SchoolModel& model,
                         const ScheduleSnapshot& snapshot) {
  return ComputeScore(ScoringContext::Build(model), snapshot);
}

ScoreReport ComputeScore(const SchoolModel& model,
                         const ScheduleSnapshot& snapshot,
                         const RatingConfig& rating) {
  return ComputeScore(ScoringContext::Build(model, rating), snapshot);
}

ScoreReport ComputeScore(const SchoolModel& model,
                         const ScheduleSnapshot& snapshot,
                         const RatingConfig& rating,
                         const RuleConfig& request_rules) {
  return ComputeScore(ScoringContext::Build(model, rating, request_rules),
                      snapshot);
}

ScoreReport ComputeScore(const ScoringContext& context,
                         const ScheduleSnapshot& snapshot) {
  return scoring::Scorer(context, snapshot).Run();
}

}  // namespace arrango
