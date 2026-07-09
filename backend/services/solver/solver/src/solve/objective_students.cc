// src/solve/objective_students.cc

#include <algorithm>
#include <map>
#include <set>
#include <vector>

#include "score/penalty_defs.h"
#include "solve/objective_internal.h"

namespace arrango {
namespace objective_detail {
namespace {

// Per-day occupancy grid for one stream: [day][period] -> covering x vars.
using CoverGrid = std::vector<std::vector<std::vector<sat::BoolVar>>>;

// Externally blocked periods for one stream: division blocks always hit;
// group blocks hit iff the group covers the stream's atom.
std::vector<std::vector<bool>> BlockedForStream(const SchoolModel& m,
    const ModelIndex& ix, const AtomSet& atoms, const StudentStream& st) {
  std::vector<std::vector<bool>> blocked(m.days.size());
  for (size_t d = 0; d < m.days.size(); ++d) {
    blocked[d].assign(m.days[d].period_count, false);
  }
  for (const ExternalBlock& blk : m.external_blocks) {
    bool hits = false;
    if (blk.target == BlockTarget::kDivision) {
      hits = ix.DivisionIdx(blk.target_id) == st.division_idx;
    }
    else if (blk.target == BlockTarget::kGroup) {
      int g = ix.GroupIdx(blk.target_id);
      hits = g >= 0 &&
             std::binary_search(atoms.of_group[g].begin(),
                                atoms.of_group[g].end(), st.atom_idx);
    }
    if (!hits) continue;
    int d = ix.DayIdx(blk.day_id);
    if (d < 0) continue;
    for (uint32_t q = blk.start_period;
         q < blk.start_period + blk.duration && q < m.days[d].period_count;
         ++q) {
      blocked[d][q] = true;
    }
  }
  return blocked;
}

// Candidate covers for one stream: the full grid plus subject-filtered grids
// for the run terms (only subjects the stream takes at least twice — a
// single lesson can never split into runs).
struct StreamCovers {
  CoverGrid all;
  std::map<Id, CoverGrid> by_subject;
};

StreamCovers CollectStreamCovers(const SchoolModel& m, const CandidateSet& cs,
    const std::vector<sat::BoolVar>& x,
    const std::vector<int>& stream_candidates,
    const std::vector<std::vector<int>>& streams_of_lesson, int stream_idx) {
  StreamCovers covers;
  covers.all.resize(m.days.size());
  for (size_t d = 0; d < m.days.size(); ++d) {
    covers.all[d].resize(m.days[d].period_count);
  }
  std::map<Id, int> subject_lessons;
  for (size_t li = 0; li < m.lessons.size(); ++li) {
    const std::vector<int>& streams = streams_of_lesson[li];
    if (std::find(streams.begin(), streams.end(), stream_idx) ==
        streams.end()) {
      continue;
    }
    ++subject_lessons[m.lessons[li].subject_id];
  }
  for (int ci : stream_candidates) {
    const Candidate& c = cs.all[ci];
    const LessonInstance& l = m.lessons[c.lesson_idx];
    for (uint32_t q = c.start; q < c.start + l.duration; ++q) {
      covers.all[c.day_idx][q].push_back(x[ci]);
    }
    if (subject_lessons[l.subject_id] >= 2) {
      CoverGrid& sc = covers.by_subject[l.subject_id];
      if (sc.empty()) {
        sc.resize(m.days.size());
        for (size_t d = 0; d < m.days.size(); ++d) {
          sc[d].resize(m.days[d].period_count);
        }
      }
      for (uint32_t q = c.start; q < c.start + l.duration; ++q) {
        sc[c.day_idx][q].push_back(x[ci]);
      }
    }
  }
  return covers;
}

}  // namespace

void Builder::StreamTerms() {
  // Streams ARE atoms: a lesson occupies a stream iff its atom set contains the
  // stream's atom. Cross-split and cross-division overlap is therefore exact
  // with no coverage machinery — an atom is busy whenever any covering lesson
  // runs.
  for (size_t s = 0; s < streams_.size(); ++s) {
    const StudentStream& st = streams_[s];
    const uint32_t priority = ix_.YearPriority(
        ix_.YearIdx(m_.divisions[st.division_idx].year_id));

    const std::vector<std::vector<bool>> blocked =
        BlockedForStream(m_, ix_, atoms_, st);
    StreamCovers stream_covers = CollectStreamCovers(
        m_, cs_, x_, candidates_of_stream_[s], streams_of_lesson_,
        static_cast<int>(s));
    const CoverGrid& covers = stream_covers.all;
    std::map<Id, CoverGrid>& subject_covers = stream_covers.by_subject;

    // Rules resolve per division (gap terms) / per subject (run terms) —
    // the same scoping the scorer uses, so modes can never drift.
    const Id year_id = m_.divisions[st.division_idx].year_id;
    const Id division_id = m_.divisions[st.division_idx].id;
    const ResolvedRule gap_rule =
        rules_.For("student_gaps", year_id, division_id, kNoId, kNoId);
    const ResolvedRule window_rule =
        rules_.For("gap_windows", year_id, division_id, kNoId, kNoId);
    std::vector<std::vector<Cell>> day_cells(m_.days.size());
    for (size_t d = 0; d < m_.days.size(); ++d) {
      day_cells[d] = MakeCells(m_.days[d].period_count, covers[d], blocked[d]);
      AddGapTerm(day_cells[d],
                 StudentWeight(gap_rule.weight > 0 ? gap_rule.weight
                                                   : w_.student_gap_base,
                               priority),
                 /*capped=*/false, gap_rule, window_rule,
                 StudentWeight(window_rule.weight > 0 ? window_rule.weight
                                                      : w_.gap_window_base,
                               priority));
    }
    for (auto& [subject_id, sc] : subject_covers) {
      const ResolvedRule run_rule =
          rules_.For("subject_once", year_id, division_id, subject_id, kNoId);
      if (run_rule.mode == RuleMode::kOff) continue;
      int subj = ix_.SubjectIdx(subject_id);
      bool prefers_blocks = subj >= 0 && m_.subjects[subj].prefers_blocks;
      int64_t weight = StudentWeight(
          run_rule.weight > 0
              ? run_rule.weight
              : (prefers_blocks ? w_.block_break_base : w_.subject_split_base),
          priority);
      for (size_t d = 0; d < m_.days.size(); ++d) {
        std::vector<bool> no_blocks(m_.days[d].period_count, false);
        AddRunTerm(MakeCells(m_.days[d].period_count, sc[d], no_blocks),
                   weight, run_rule);
      }
    }
    MaxLessonsTerms(st, static_cast<int>(s), day_cells, covers, blocked,
                    priority);
    DailyLoadTerms(st, day_cells, priority);
  }
}

// CP encoding of the scorer's ScoreDailyLoad: per active day, over/under
// variables channel max(0, load - target - allowed) and
// max(0, target - allowed - load); deviation is their sum (at most one is
// nonzero). Imbalance channels (max - min) of the active days' loads. Day
// cells already include external blocks, matching the scorer's occupancy.
void Builder::DailyLoadTerms(const StudentStream& st,
                             const std::vector<std::vector<Cell>>& day_cells,
                             uint32_t priority) {
  if (m_.daily_load_rules.empty()) return;
  const Id year_id = m_.divisions[st.division_idx].year_id;
  const Id division_id = m_.divisions[st.division_idx].id;
  const ResolvedRule band_rule =
      rules_.For("daily_band", year_id, division_id, kNoId, kNoId);
  const ResolvedRule imbalance_rule =
      rules_.For("daily_imbalance", year_id, division_id, kNoId, kNoId);
  const bool band_hard = band_rule.mode == RuleMode::kHard;
  const bool imb_hard = imbalance_rule.mode == RuleMode::kHard;
  const EffectiveDailyLoad& eff = DailyLoadOf(st.division_idx);
  const int64_t dev_w = band_rule.mode == RuleMode::kSoft
      ? DailyLoadWeight(eff.deviation_weight, priority) : 0;
  const int64_t over_w = band_rule.mode == RuleMode::kSoft
      ? DailyLoadWeight(eff.overload_weight, priority) : 0;
  const int64_t under_w = band_rule.mode == RuleMode::kSoft
      ? DailyLoadWeight(eff.underload_weight, priority) : 0;
  const int64_t imb_w = imbalance_rule.mode == RuleMode::kSoft
      ? DailyLoadWeight(eff.imbalance_weight, priority) : 0;
  if (dev_w == 0 && over_w == 0 && under_w == 0 && imb_w == 0 &&
      !band_hard && !imb_hard) {
    return;
  }
  const int64_t target = static_cast<int64_t>(eff.target_per_day);
  const int64_t allowed = static_cast<int64_t>(eff.allowed_deviation);

  std::vector<sat::LinearExpr> day_loads;
  int64_t load_bound = 0;
  for (size_t d = 0; d < day_cells.size(); ++d) {
    if (m_.days[d].period_count < 1) continue;  // inactive day
    sat::LinearExpr load;
    int64_t upper = 0;
    for (const Cell& c : day_cells[d]) {
      if (c.state == Cell::kTrue) load += 1;
      if (c.state == Cell::kVar) load += c.var;
      if (c.state != Cell::kFalse) ++upper;
    }
    if (band_hard) {
      // HARD band: every active day must sit inside [target - allowed,
      // target + allowed] — the soft over/under variables become bounds.
      cp_.AddLessOrEqual(load, target + allowed);
      if (target - allowed > 0) {
        cp_.AddGreaterOrEqual(load, target - allowed);
      }
    }
    // over == max(0, load - target - allowed): how many periods this day
    // exceeds the tolerated band. Skipped when even a full day can't exceed
    // it (upper bound proves over == 0, matching the scorer's zero).
    if (!band_hard && (dev_w > 0 || over_w > 0) && upper > target + allowed) {
      sat::IntVar over = cp_.NewIntVar({ 0, upper - target - allowed });
      cp_.AddMaxEquality(over, std::vector<sat::LinearExpr>{
          load - (target + allowed), sat::LinearExpr(0)});
      // deviation = over + under (only one side can be nonzero), so dev_w
      // rides on the same variable instead of encoding |load - target|.
      objective_ += (dev_w + over_w) * over;
    }
    // under == max(0, target - allowed - load): shortfall below the band.
    // Skipped when the band floor is <= 0 (under can never be positive).
    if (!band_hard && (dev_w > 0 || under_w > 0) && target - allowed > 0) {
      sat::IntVar under = cp_.NewIntVar({ 0, target - allowed });
      cp_.AddMaxEquality(under, std::vector<sat::LinearExpr>{
          (target - allowed) - load, sat::LinearExpr(0)});
      objective_ += (dev_w + under_w) * under;
    }
    day_loads.push_back(std::move(load));
    load_bound = std::max(load_bound, upper);
  }
  // imbalance == (heaviest day) - (lightest day) across ACTIVE days only;
  // the equality constraints pin all three vars to exact values, so the
  // objective term equals the scorer's max-min computation. HARD bounds the
  // spread by allowed_deviation instead of pricing it.
  if ((imb_w > 0 || imb_hard) && !day_loads.empty()) {
    sat::IntVar max_load = cp_.NewIntVar({ 0, load_bound });
    cp_.AddMaxEquality(max_load, day_loads);
    sat::IntVar min_load = cp_.NewIntVar({ 0, load_bound });
    cp_.AddMinEquality(min_load, day_loads);
    if (imb_hard) {
      cp_.AddLessOrEqual(max_load - min_load, allowed);
    }
    else {
      sat::IntVar imbalance = cp_.NewIntVar({ 0, load_bound });
      cp_.AddEquality(imbalance, max_load - min_load);
      objective_ += imb_w * imbalance;
    }
  }
}

void Builder::MaxLessonsTerms(const StudentStream& st, int stream_idx,
                              const std::vector<std::vector<Cell>>& day_cells,
                              const std::vector<std::vector<std::vector<sat::BoolVar>>>& covers,
                              const std::vector<std::vector<bool>>& blocked,
                              uint32_t priority) {
  const Id year_id = m_.divisions[st.division_idx].year_id;
  const Id division_id = m_.divisions[st.division_idx].id;
  const ResolvedRule rule =
      rules_.For("max_lessons", year_id, division_id, kNoId, kNoId);
  if (rule.mode == RuleMode::kOff) return;
  // "Przedmiot extra": subject-scoped OFF exempts that subject's periods.
  // Rebuild the day cells from covers WITHOUT the exempt candidates —
  // blocked periods keep counting, matching the scorer's filtered union.
  std::set<Id> exempt_subjects;
  for (int ci : candidates_of_stream_[stream_idx]) {
    const Id subject = m_.lessons[cs_.all[ci].lesson_idx].subject_id;
    if (exempt_subjects.count(subject)) continue;
    if (rules_.For("max_lessons", year_id, division_id, subject, kNoId)
            .mode == RuleMode::kOff) {
      exempt_subjects.insert(subject);
    }
  }
  std::vector<std::vector<Cell>> filtered;
  const std::vector<std::vector<Cell>>* cells = &day_cells;
  if (!exempt_subjects.empty()) {
    std::vector<std::vector<std::vector<sat::BoolVar>>> kept(m_.days.size());
    for (size_t d = 0; d < m_.days.size(); ++d) {
      kept[d].resize(m_.days[d].period_count);
    }
    for (int ci : candidates_of_stream_[stream_idx]) {
      const Candidate& c = cs_.all[ci];
      const LessonInstance& l = m_.lessons[c.lesson_idx];
      if (exempt_subjects.count(l.subject_id)) continue;
      for (uint32_t q = c.start; q < c.start + l.duration; ++q) {
        kept[c.day_idx][q].push_back(x_[ci]);
      }
    }
    filtered.resize(m_.days.size());
    for (size_t d = 0; d < m_.days.size(); ++d) {
      filtered[d] = MakeCells(m_.days[d].period_count, kept[d], blocked[d]);
    }
    cells = &filtered;
  }
  for (const Preference& pref : m_.preferences) {
    if (pref.kind != PreferenceKind::kMaxLessonsPerDay) continue;
    if (!PrefMatchesDivision(m_, pref, st.division_idx)) continue;
    const int64_t weight =
        StudentWeight(rule.weight > 0 ? rule.weight : pref.weight, priority);
    if (rule.mode != RuleMode::kHard && weight == 0) continue;
    for (size_t d = 0; d < cells->size(); ++d) {
      sat::LinearExpr load;
      int64_t upper = 0;
      for (const Cell& c : (*cells)[d]) {
        if (c.state == Cell::kTrue) load += 1;
        if (c.state == Cell::kVar) load += c.var;
        if (c.state != Cell::kFalse) ++upper;
      }
      int64_t max_excess = upper - static_cast<int64_t>(pref.param);
      if (max_excess <= 0) continue;
      if (rule.mode == RuleMode::kHard) {
        // HARD: the preference's limit becomes a bound instead of a cost.
        cp_.AddLessOrEqual(load, static_cast<int64_t>(pref.param));
        continue;
      }
      sat::IntVar excess = cp_.NewIntVar({ 0, max_excess });
      cp_.AddMaxEquality(
          excess, std::vector<sat::LinearExpr>{
          load - static_cast<int64_t>(pref.param), sat::LinearExpr(0)});
      objective_ += weight * excess;
    }
  }
}

}  // namespace objective_detail
}  // namespace arrango
