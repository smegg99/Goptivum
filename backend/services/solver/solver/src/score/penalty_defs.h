// src/score/penalty_defs.h

#pragma once

#include <algorithm>
#include <cstdint>
#include <string>

#include "model/daily_load.h"
#include "model/index.h"
#include "model/model.h"

namespace arrango {

  // Single source of penalty semantics shared by the scorer and the CP-SAT
  // objective builder so the two can never drift.

  // Student-facing penalties are multiplied by year priority (percent).
  inline int64_t StudentWeight(int64_t base, uint32_t year_priority) {
    return base * static_cast<int64_t>(year_priority) / 100;
  }

  // Cost of one occupied period at 0-based index `p`; grows with lateness.
  inline int64_t LatePeriodCost(int64_t base, uint32_t p, uint32_t threshold) {
    return p >= threshold ? base * static_cast<int64_t>(p - threshold + 1) : 0;
  }

  // Replaces zero-valued fields (proto3 "unset") with the built-in defaults.
  inline Weights WithDefaults(Weights w) {
    const Weights d;
    if (w.student_gap_base == 0) w.student_gap_base = d.student_gap_base;
    if (w.teacher_gap_base == 0) w.teacher_gap_base = d.teacher_gap_base;
    if (w.late_student_lesson_base == 0)
      w.late_student_lesson_base = d.late_student_lesson_base;
    if (w.late_teacher_finish_base == 0)
      w.late_teacher_finish_base = d.late_teacher_finish_base;
    if (w.subject_split_base == 0) w.subject_split_base = d.subject_split_base;
    if (w.block_break_base == 0) w.block_break_base = d.block_break_base;
    if (w.room_change_base == 0) w.room_change_base = d.room_change_base;
    if (w.stability_move_base == 0)
      w.stability_move_base = d.stability_move_base;
    if (w.expected_bad_per_lesson == 0)
      w.expected_bad_per_lesson = d.expected_bad_per_lesson;
    if (w.late_threshold_period == 0)
      w.late_threshold_period = d.late_threshold_period;
    if (w.gap_cap_per_day == 0) w.gap_cap_per_day = d.gap_cap_per_day;
    if (w.gap_window_base == 0) w.gap_window_base = d.gap_window_base;
    // early_start_base is NOT defaulted: 0 means the preference is off.
    return w;
  }

  // Soft daily-load counts for one stream-day load against an effective rule:
  // `overload`/`underload` count periods beyond target ± allowed_deviation in
  // their direction; `deviation` is the direction-agnostic total. At most one
  // direction is nonzero, so deviation == overload + underload — the CP-SAT
  // encoding relies on that identity to reuse its over/under variables.
  struct DailyLoadCounts {
    int64_t deviation{};
    int64_t overload{};
    int64_t underload{};
  };

  inline DailyLoadCounts DailyLoadDayCounts(int64_t load,
    const EffectiveDailyLoad& eff) {
    const int64_t target = static_cast<int64_t>(eff.target_per_day);
    const int64_t allowed = static_cast<int64_t>(eff.allowed_deviation);
    DailyLoadCounts counts;
    counts.overload = std::max<int64_t>(0, load - target - allowed);
    counts.underload = std::max<int64_t>(0, target - allowed - load);
    counts.deviation = counts.overload + counts.underload;
    return counts;
  }

  // Daily-load weights are user data; anything non-positive means "off" (a
  // negative weight would reward violations and break the CP encoding).
  inline int64_t DailyLoadWeight(int64_t base, uint32_t year_priority) {
    return std::max<int64_t>(0, StudentWeight(base, year_priority));
  }

  // Hard upper per-day load bound for a division from its total weekly periods
  // over its active days: the fallback maximum used when no rule sets one, so
  // no single day crams far above its even share.
  inline int64_t DayLoadMax(int64_t total_periods, int64_t days) {
    if (days <= 0) return total_periods;
    return (total_periods + days - 1) / days + 3;
  }

  // Preference targeting, shared by scorer and objective so the two can
  // never drift. A lesson matches when ANY participant's division matches.
  inline bool PrefMatchesDivision(const SchoolModel& m, const Preference& pref,
    int division_idx) {
    const Division& d = m.divisions[division_idx];
    if (pref.division_id != kNoId && pref.division_id != d.id) return false;
    if (pref.year_id != kNoId && pref.year_id != d.year_id) return false;
    return true;
  }

  inline bool PrefMatchesLesson(const SchoolModel& m, const ModelIndex& ix,
    const Preference& pref,
    const LessonInstance& l) {
    if (pref.subject_id != kNoId && pref.subject_id != l.subject_id) {
      return false;
    }
    for (const Participant& p : l.participants) {
      int c = ix.DivisionIdx(p.division_id);
      if (c >= 0 && PrefMatchesDivision(m, pref, c)) return true;
    }
    return false;
  }

  // Student-priority multiplier of a lesson: the highest year priority among
  // its participants (merged lessons inherit the most important cohort).
  inline uint32_t LessonPriority(const SchoolModel& m, const ModelIndex& ix,
    const LessonInstance& l) {
    uint32_t best = 100;
    bool any = false;
    for (const Participant& p : l.participants) {
      int c = ix.DivisionIdx(p.division_id);
      if (c < 0) continue;
      uint32_t prio = ix.YearPriority(ix.YearIdx(m.divisions[c].year_id));
      if (!any || prio > best) best = prio;
      any = true;
    }
    return best;
  }

  // Penalty breakdown category names.
  inline constexpr const char* kCatStudentGap = "student_gap";
  inline constexpr const char* kCatTeacherGap = "teacher_gap";
  inline constexpr const char* kCatLateStudent = "late_student";
  inline constexpr const char* kCatLateTeacher = "late_teacher";
  inline constexpr const char* kCatSubjectSplit = "subject_split";
  inline constexpr const char* kCatBlockBreak = "block_break";
  inline constexpr const char* kCatRoomChange = "room_change";
  inline constexpr const char* kCatStability = "stability";
  inline constexpr const char* kCatMaxLessons = "max_lessons";
  inline constexpr const char* kCatPreferEarly = "prefer_early";
  inline constexpr const char* kCatGapWindow = "gap_window";
  inline constexpr const char* kCatDailyDeviation = "daily_deviation";
  inline constexpr const char* kCatDailyOverload = "daily_overload";
  inline constexpr const char* kCatDailyUnderload = "daily_underload";
  inline constexpr const char* kCatDailyImbalance = "daily_imbalance";
  // Metric-only category (zero penalty, never in the objective): an active
  // teacher-day holding exactly one lesson instance.
  inline constexpr const char* kCatSingleLessonDay = "single_lesson_day";
  // A teacher scheduled in BOTH the first and last `param` periods of one
  // day (rule anti_split_shift; default off, dobry_plan profile enables).
  inline constexpr const char* kCatAntiSplitShift = "anti_split_shift";
  // A school day with NO lesson for a teacher whose teach_daily rule is on
  // (Dobry Plan "codziennie"; default off).
  inline constexpr const char* kCatTeachDaily = "teach_daily";
  // Active days beyond the few_days rule's param for one teacher (Dobry
  // Plan "celebryta": compress presence into few days; default off).
  inline constexpr const char* kCatFewDays = "few_days";
  // Hygiene (INFO) categories: reported in ScoreReport::info_issues only —
  // never scored, never in the objective, never blocking a pristine verdict.
  inline constexpr const char* kCatStartVariance = "start_variance";
  inline constexpr const char* kCatLateFirstLesson = "late_first_lesson";
  inline constexpr const char* kCatSubjectAlwaysLast = "subject_always_last";

}  // namespace arrango
