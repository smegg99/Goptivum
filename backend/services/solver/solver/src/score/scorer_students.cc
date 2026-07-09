// src/score/scorer_students.cc

#include <algorithm>
#include <map>
#include <set>
#include <string>
#include <vector>

#include "score/penalty_defs.h"
#include "score/scorer_internal.h"

namespace arrango {
namespace scoring {

void Scorer::ScoreStreams() {
  for (size_t s_i = 0; s_i < streams_.size(); ++s_i) {
    const StudentStream& st = streams_[s_i];
    CatMap& cats = division_cats_[st.division_idx];
    const uint32_t priority = StreamPriority(st);
    auto occ = StreamOccupancy(s_i);

    ScoreStreamGaps(s_i, occ, cats, priority);
    ScoreLateLessons(s_i, cats, priority);
    ScoreSubjectRuns(s_i, cats, priority);
    ScoreMaxLessons(s_i, occ, cats, priority);
    ScoreDailyLoad(s_i, occ, cats, priority);
  }
}

// Student gaps are uncapped (near-hard by default); the cap applies to
// teacher gaps only. Multi-period windows escalate per adjacent empty pair —
// their default mode is HARD, so solver output never reaches that branch,
// imported schedules do.
void Scorer::ScoreStreamGaps(size_t s_i,
                             const std::vector<std::vector<bool>>& occ,
                             CatMap& cats, uint32_t priority) {
  const StudentStream& st = streams_[s_i];
  const Id division_id = m_.divisions[st.division_idx].id;
  const ResolvedRule gap_rule = DivisionRule("student_gaps", st.division_idx);
  const ResolvedRule window_rule =
      DivisionRule("gap_windows", st.division_idx);
  for (size_t d = 0; d < occ.size(); ++d) {
    GapInfo gaps = Gaps(occ[d]);
    ApplyRule(gap_rule, cats, kCatStudentGap,
              gaps.count * StudentWeight(WeightOr(gap_rule,
                                                  w_.student_gap_base),
                                         priority),
              static_cast<uint32_t>(gaps.count), st.label, division_id,
              false, static_cast<int>(d),
              gaps.first_gap >= 0 ? gaps.first_gap : 0,
              StreamDayRefs(s_i, static_cast<int>(d)));
    ApplyRule(window_rule, cats, kCatGapWindow,
              gaps.window_pairs * StudentWeight(WeightOr(window_rule,
                                                         w_.gap_window_base),
                                                priority),
              static_cast<uint32_t>(gaps.window_pairs), st.label,
              division_id, false, static_cast<int>(d),
              gaps.first_gap >= 0 ? gaps.first_gap : 0);
  }
}

// Late lessons: per member placement so multipliers match the CP-SAT
// per-candidate costs exactly; issues aggregate per day. The rule is
// subject-scoped ("trudny" subjects can be harder than others), so each
// placement resolves its own mode.
void Scorer::ScoreLateLessons(size_t s_i, CatMap& cats, uint32_t priority) {
  const StudentStream& st = streams_[s_i];
  const Id division_id = m_.divisions[st.division_idx].id;
  std::vector<int64_t> late_by_day(m_.days.size(), 0);
  std::vector<uint32_t> late_count(m_.days.size(), 0);
  std::vector<uint32_t> hard_count(m_.days.size(), 0);
  std::vector<uint32_t> late_first(m_.days.size(), 0);
  for (const Placed* p : stream_members_[s_i]) {
    const ResolvedRule late_rule = DivisionRule(
        "late_student", st.division_idx, m_.lessons[p->lesson_idx].subject_id);
    // Rating denominators ride along: total and late stream-member
    // periods per division (the lateness metric's rate) — counted in
    // every mode, the metric observes regardless.
    student_periods_of_division_[st.division_idx] += p->duration;
    int64_t cost = 0;
    uint32_t placement_late = 0;
    for (uint32_t q = p->start; q < p->start + p->duration; ++q) {
      int64_t at = StudentWeight(
          LatePeriodCost(WeightOr(late_rule, w_.late_student_lesson_base),
                         q, w_.late_threshold_period),
          priority);
      cost += at;
      if (at > 0) {
        ++placement_late;
        ++late_student_periods_of_division_[st.division_idx];
        if (late_count[p->day_idx] == 0 || q < late_first[p->day_idx]) {
          late_first[p->day_idx] = q;
        }
        ++late_count[p->day_idx];
      }
    }
    switch (late_rule.mode) {
    case RuleMode::kSoft:
    case RuleMode::kDefault:
      late_by_day[p->day_idx] += cost;
      AddPenalty(cats, kCatLateStudent, cost);
      break;
    case RuleMode::kHard:
      AddCount(cats, kCatLateStudent, placement_late);
      hard_count[p->day_idx] += placement_late;
      break;
    case RuleMode::kOff:
      AddCount(cats, kCatLateStudent, placement_late);
      break;
    }
  }
  for (size_t d = 0; d < m_.days.size(); ++d) {
    AddIssue(kCatLateStudent, st.label, division_id, false,
             static_cast<int>(d), late_first[d], late_count[d],
             late_by_day[d], StreamDayRefs(s_i, static_cast<int>(d)));
    AddIssue(kCatLateStudent, st.label, division_id, false,
             static_cast<int>(d), late_first[d], hard_count[d],
             /*penalty=*/0, StreamDayRefs(s_i, static_cast<int>(d)),
             /*config_hard=*/true);
  }
}

// Same-day contiguity: a subject placed as N separate runs on one day
// costs (N - 1) split penalties. Matches the CP-SAT run-start encoding.
// Subjects that prefer blocks pay the heavier block_break weight.
void Scorer::ScoreSubjectRuns(size_t s_i, CatMap& cats, uint32_t priority) {
  std::map<Id, std::vector<const Placed*>> by_subject;
  for (const Placed* p : stream_members_[s_i]) {
    by_subject[m_.lessons[p->lesson_idx].subject_id].push_back(p);
  }
  for (const auto& [subject_id, lessons] : by_subject) {
    if (lessons.size() < 2) continue;
    int subj = ix_.SubjectIdx(subject_id);
    bool prefers_blocks = subj >= 0 && m_.subjects[subj].prefers_blocks;
    for (size_t d = 0; d < m_.days.size(); ++d) {
      std::vector<bool> occ(m_.days[d].period_count, false);
      for (const Placed* p : lessons) {
        if (p->day_idx != static_cast<int>(d)) continue;
        for (uint32_t q = p->start; q < p->start + p->duration; ++q) {
          occ[q] = true;
        }
      }
      int runs = 0;
      for (size_t q = 0; q < occ.size(); ++q) {
        if (occ[q] && (q == 0 || !occ[q - 1])) ++runs;
      }
      if (runs <= 1) continue;
      // subject_once is subject-scoped: hard school-wide with an OFF
      // override for one subject == Dobry Plan's "podwójna dawka".
      const ResolvedRule rule = DivisionRule(
          "subject_once", streams_[s_i].division_idx, subject_id);
      int64_t base =
          prefers_blocks ? w_.block_break_base : w_.subject_split_base;
      int64_t penalty =
          (runs - 1) * StudentWeight(WeightOr(rule, base), priority);
      const char* cat = prefers_blocks ? kCatBlockBreak : kCatSubjectSplit;
      uint32_t first_q = 0;
      for (size_t q = 0; q < occ.size(); ++q) {
        if (occ[q]) {
          first_q = static_cast<uint32_t>(q);
          break;
        }
      }
      std::string subject_name = subj >= 0 ? m_.subjects[subj].name : "?";
      // Point at this subject's blocks on the split day.
      std::vector<LessonRef> refs;
      for (const Placed* p : lessons) {
        if (p->day_idx == static_cast<int>(d)) refs.push_back(RefOf(p));
      }
      ApplyRule(rule, cats, cat, penalty, static_cast<uint32_t>(runs - 1),
                streams_[s_i].label + " · " + subject_name,
                m_.divisions[streams_[s_i].division_idx].id, false,
                static_cast<int>(d), first_q, std::move(refs));
    }
  }
}

// Soft daily-load terms, per stream: deviation/overload/underload against the
// division's effective target on each active day, plus (max - min) imbalance
// across active days. Loads count the same occupancy the gap terms use —
// external blocks included — so the CP-SAT encoding can reuse its day cells.
void Scorer::ScoreDailyLoad(size_t s_i,
                            const std::vector<std::vector<bool>>& occ,
                            CatMap& cats, uint32_t priority) {
  if (m_.daily_load_rules.empty()) return;
  const StudentStream& st = streams_[s_i];
  // Band weights stay rule-driven (DailyLoadRule); the rule mode gates the
  // whole family. Hard/off refinement of individual band terms would need
  // per-term rules — YAGNI until asked.
  const ResolvedRule band_rule = DivisionRule("daily_band", st.division_idx);
  const ResolvedRule imbalance_rule =
      DivisionRule("daily_imbalance", st.division_idx);
  if (band_rule.mode == RuleMode::kOff &&
      imbalance_rule.mode == RuleMode::kOff) {
    return;
  }
  const EffectiveDailyLoad& eff = DailyLoadOf(st.division_idx);
  const int64_t dev_w = DailyLoadWeight(eff.deviation_weight, priority);
  const int64_t over_w = DailyLoadWeight(eff.overload_weight, priority);
  const int64_t under_w = DailyLoadWeight(eff.underload_weight, priority);
  const int64_t imb_w = DailyLoadWeight(eff.imbalance_weight, priority);
  // Zero weights = the family is silent, UNLESS a hard mode still needs the
  // violations counted and reported as config-hard errors.
  if (dev_w == 0 && over_w == 0 && under_w == 0 && imb_w == 0 &&
      band_rule.mode != RuleMode::kHard &&
      imbalance_rule.mode != RuleMode::kHard) {
    return;
  }

  const Id division_id = m_.divisions[st.division_idx].id;
  int64_t max_load = -1, min_load = -1;  // -1 = no active day seen yet
  for (size_t d = 0; d < occ.size(); ++d) {
    if (m_.days[d].period_count < 1) continue;  // inactive day
    // Day load = occupied periods, external blocks included (same occupancy
    // the gap terms use; the CP-SAT side reuses its day cells for parity).
    const int64_t load = std::count(occ[d].begin(), occ[d].end(), true);
    if (max_load < 0 || load > max_load) max_load = load;
    if (min_load < 0 || load < min_load) min_load = load;
    const DailyLoadCounts counts = DailyLoadDayCounts(load, eff);
    ApplyRule(band_rule, cats, kCatDailyDeviation, dev_w * counts.deviation,
              static_cast<uint32_t>(counts.deviation), st.label, division_id,
              false, static_cast<int>(d), 0);
    ApplyRule(band_rule, cats, kCatDailyOverload, over_w * counts.overload,
              static_cast<uint32_t>(counts.overload), st.label, division_id,
              false, static_cast<int>(d), 0);
    ApplyRule(band_rule, cats, kCatDailyUnderload, under_w * counts.underload,
              static_cast<uint32_t>(counts.underload), st.label, division_id,
              false, static_cast<int>(d), 0);
  }
  if (max_load >= 0) {
    const int64_t imbalance = max_load - min_load;
    ApplyRule(imbalance_rule, cats, kCatDailyImbalance, imb_w * imbalance,
              static_cast<uint32_t>(imbalance), st.label, division_id, false,
              -1, 0);
  }
}

// Per-lesson costs (stability, prefer-early) attributed to the lesson's
// FIRST participant's division: exactly once per lesson so totals stay
// penalty-identical with the CP-SAT objective.
void Scorer::ScoreLessonCosts() {
  for (const Placed& p : placed_) {
    const LessonInstance& l = m_.lessons[p.lesson_idx];
    if (l.participants.empty()) continue;
    int c = ix_.DivisionIdx(l.participants.front().division_id);
    if (c < 0) continue;
    uint32_t priority = LessonPriority(m_, ix_, l);
    // Both are soft-only rules (hard makes no sense); off silences them.
    const ResolvedRule early_rule =
        DivisionRule("prefer_early", c, l.subject_id);
    const ResolvedRule stability_rule = DivisionRule("stability", c);
    if (early_rule.mode != RuleMode::kOff && w_.early_start_base > 0) {
      AddPenalty(division_cats_[c], kCatPreferEarly,
                 StudentWeight(w_.early_start_base * p.start, priority));
    }
    if (stability_rule.mode != RuleMode::kOff && l.previous_placement &&
        !(p.placement == *l.previous_placement)) {
      const int64_t move =
          WeightOr(stability_rule, w_.stability_move_base);
      AddPenalty(division_cats_[c], kCatStability, move);
      AddIssue(kCatStability, m_.divisions[c].name, m_.divisions[c].id, false,
               p.day_idx, p.start, 1, move, { RefOf(&p) });
    }
    if (early_rule.mode == RuleMode::kOff) continue;
    for (const Preference& pref : m_.preferences) {
      if (pref.kind != PreferenceKind::kPreferEarly ||
          !PrefMatchesLesson(m_, ix_, pref, l)) {
        continue;
      }
      AddPenalty(division_cats_[c], kCatPreferEarly,
                 StudentWeight(pref.weight * p.start, priority));
    }
  }
}

void Scorer::ScoreMaxLessons(size_t s_i,
                             const std::vector<std::vector<bool>>& occ,
                             CatMap& cats, uint32_t priority) {
  const ResolvedRule rule =
      DivisionRule("max_lessons", streams_[s_i].division_idx);
  if (rule.mode == RuleMode::kOff) return;
  // "Przedmiot extra": a subject-scoped OFF override exempts that
  // subject's periods from the count. Filtered load = periods covered by
  // >= 1 NON-exempt lesson, plus externally blocked periods (occ minus any
  // lesson cover) — the same union the CP-SAT cells encode.
  std::set<Id> exempt_subjects;
  std::vector<std::vector<bool>> nonexempt_cover, any_cover;
  for (const Placed* p : stream_members_[s_i]) {
    const Id subject = m_.lessons[p->lesson_idx].subject_id;
    if (exempt_subjects.count(subject)) continue;
    if (DivisionRule("max_lessons", streams_[s_i].division_idx, subject)
            .mode == RuleMode::kOff) {
      exempt_subjects.insert(subject);
    }
  }
  if (!exempt_subjects.empty()) {
    nonexempt_cover.resize(m_.days.size());
    any_cover.resize(m_.days.size());
    for (size_t d = 0; d < m_.days.size(); ++d) {
      nonexempt_cover[d].assign(m_.days[d].period_count, false);
      any_cover[d].assign(m_.days[d].period_count, false);
    }
    for (const Placed* p : stream_members_[s_i]) {
      const bool is_exempt =
          exempt_subjects.count(m_.lessons[p->lesson_idx].subject_id) > 0;
      for (uint32_t q = p->start; q < p->start + p->duration; ++q) {
        any_cover[p->day_idx][q] = true;
        if (!is_exempt) nonexempt_cover[p->day_idx][q] = true;
      }
    }
  }
  for (const Preference& pref : m_.preferences) {
    if (pref.kind != PreferenceKind::kMaxLessonsPerDay) continue;
    // Stream prefs match by division/year only; subject filter ignored.
    if (!PrefMatchesDivision(m_, pref, streams_[s_i].division_idx)) {
      continue;
    }
    for (size_t d = 0; d < occ.size(); ++d) {
      const auto& day = occ[d];
      int64_t used = 0;
      if (exempt_subjects.empty()) {
        used = std::count(day.begin(), day.end(), true);
      }
      else {
        for (size_t q = 0; q < day.size(); ++q) {
          used += nonexempt_cover[d][q] || (day[q] && !any_cover[d][q]);
        }
      }
      int64_t excess = used - static_cast<int64_t>(pref.param);
      if (excess <= 0) continue;
      if (rule.mode == RuleMode::kHard) {
        AddCount(cats, kCatMaxLessons, static_cast<uint32_t>(excess));
        AddIssue(kCatMaxLessons, streams_[s_i].label,
                 m_.divisions[streams_[s_i].division_idx].id, false, -1, 0,
                 static_cast<uint32_t>(excess), 0, {}, /*config_hard=*/true);
      }
      else {
        AddPenalty(cats, kCatMaxLessons,
                   excess * StudentWeight(WeightOr(rule, pref.weight),
                                          priority),
                   static_cast<uint32_t>(excess));
      }
    }
  }
}

}  // namespace scoring
}  // namespace arrango
