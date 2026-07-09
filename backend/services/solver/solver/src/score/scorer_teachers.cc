// src/score/scorer_teachers.cc

#include <set>
#include <vector>

#include "score/penalty_defs.h"
#include "score/scorer_internal.h"

namespace arrango {
namespace scoring {

void Scorer::ScoreTeachers() {
  single_days_of_teacher_.assign(m_.teachers.size(), 0);
  for (size_t t = 0; t < m_.teachers.size(); ++t) {
    CatMap& cats = teacher_cats_[t];
    // Teacher rules resolve per teacher: per-teacher overrides are Dobry
    // Plan's comfort flags (uczulony na okienka = heavier gap weight,
    // elastyczny = rules off) done as data.
    const Id teacher_id = m_.teachers[t].id;
    auto teacher_rule = [&] (const char* rule) {
      return rules_.For(rule, kNoId, kNoId, kNoId, teacher_id);
      };
    std::vector<std::vector<bool>> occ(m_.days.size());
    for (size_t d = 0; d < m_.days.size(); ++d) {
      occ[d].assign(m_.days[d].period_count, false);
    }
    std::vector<std::set<int>> rooms_used(m_.days.size());
    std::vector<bool> active(m_.days.size(), false);
    std::vector<int> lessons_per_day(m_.days.size(), 0);
    std::vector<std::vector<LessonRef>> day_refs(m_.days.size());
    for (const Placed& p : placed_) {
      const LessonInstance& l = m_.lessons[p.lesson_idx];
      if (!l.requires_teacher ||
          ix_.TeacherIdx(l.teacher_id) != static_cast<int>(t)) {
        continue;
      }
      active[p.day_idx] = true;
      ++lessons_per_day[p.day_idx];
      day_refs[p.day_idx].push_back(RefOf(&p));
      if (p.room_idx >= 0) rooms_used[p.day_idx].insert(p.room_idx);
      periods_of_teacher_[t] += p.duration;
      const ResolvedRule late_rule = teacher_rule("late_teacher");
      int64_t late = 0;
      uint32_t late_count = 0;
      for (uint32_t q = p.start; q < p.start + p.duration; ++q) {
        occ[p.day_idx][q] = true;
        const int64_t at = LatePeriodCost(
            WeightOr(late_rule, w_.late_teacher_finish_base), q,
            w_.late_threshold_period);
        late += at;
        if (at > 0) {
          ++late_periods_of_teacher_[t];
          ++late_count;
        }
      }
      ApplyRule(late_rule, cats, kCatLateTeacher, late, late_count,
                m_.teachers[t].name, teacher_id, true, p.day_idx, p.start);
    }
    for (const ExternalBlock& blk : m_.external_blocks) {
      if (blk.target != BlockTarget::kTeacher ||
          ix_.TeacherIdx(blk.target_id) != static_cast<int>(t)) {
        continue;
      }
      int d = ix_.DayIdx(blk.day_id);
      if (d < 0) continue;
      for (uint32_t q = blk.start_period;
           q < blk.start_period + blk.duration && q < m_.days[d].period_count;
           ++q) {
        occ[d][q] = true;
      }
    }
    const ResolvedRule gap_rule = teacher_rule("teacher_gaps");
    const ResolvedRule room_rule = teacher_rule("room_changes");
    for (size_t d = 0; d < occ.size(); ++d) {
      GapInfo info = Gaps(occ[d]);
      int64_t gaps = std::min<int64_t>(info.count, w_.gap_cap_per_day);
      ApplyRule(gap_rule, cats, kCatTeacherGap,
                gaps * WeightOr(gap_rule, w_.teacher_gap_base),
                static_cast<uint32_t>(gaps), m_.teachers[t].name, teacher_id,
                true, static_cast<int>(d),
                info.first_gap >= 0 ? info.first_gap : 0, day_refs[d]);
      if (active[d] && rooms_used[d].size() > 1) {
        int64_t changes = static_cast<int64_t>(rooms_used[d].size()) - 1;
        ApplyRule(room_rule, cats, kCatRoomChange,
                  changes * WeightOr(room_rule, w_.room_change_base),
                  static_cast<uint32_t>(changes), m_.teachers[t].name,
                  teacher_id, true, static_cast<int>(d), 0, day_refs[d]);
      }
      if (active[d]) ++active_days_of_teacher_[t];
      // A day holding exactly one lesson. The metric counter observes in
      // every mode; the issue/penalty follow the rule (soft with weight 0 —
      // the default — keeps today's zero-penalty warning).
      if (lessons_per_day[d] == 1) {
        ++single_days_of_teacher_[t];
        const ResolvedRule lonely_rule = teacher_rule("single_lesson_day");
        if (lonely_rule.mode != RuleMode::kOff) {
          const bool hard = lonely_rule.mode == RuleMode::kHard;
          const int64_t pen = hard
              ? 0 : WeightOr(lonely_rule, w_.single_lesson_day_base);
          AddPenalty(cats, kCatSingleLessonDay, pen);
          SoftIssue lonely{ kCatSingleLessonDay, m_.teachers[t].name,
                           m_.teachers[t].id, true, m_.days[d].id, 0, 1, pen,
                           hard, {} };
          lonely.locus.entities.push_back(
              { EntityKind::kTeacher, m_.teachers[t].id });
          lonely.locus.lessons = day_refs[d];
          issues_.push_back(std::move(lonely));
        }
      }
      // teach_daily ("codziennie"): an ACTIVE school day with no lesson for
      // this teacher. Weight comes from the rule alone.
      const ResolvedRule daily_rule = teacher_rule("teach_daily");
      if (daily_rule.mode != RuleMode::kOff &&
        m_.days[d].period_count >= 1 && lessons_per_day[d] == 0) {
        ApplyRule(daily_rule, cats, kCatTeachDaily, daily_rule.weight, 1,
                  m_.teachers[t].name, teacher_id, true, static_cast<int>(d),
                  0);
      }
      // anti_split_shift: lessons in BOTH the first and last `edge` periods
      // of one day (only meaningful when the day is longer than both
      // edges). Weight comes from the rule alone — no Weights base.
      const ResolvedRule shift_rule = teacher_rule("anti_split_shift");
      if (shift_rule.mode != RuleMode::kOff && active[d]) {
        const uint32_t edge = shift_rule.param > 0
            ? shift_rule.param : kAntiSplitShiftDefaultEdge;
        const uint32_t periods = m_.days[d].period_count;
        if (periods > 2 * edge) {
          bool early = false, late = false;
          for (const LessonRef& ref : day_refs[d]) {
            if (ref.start_period < edge) early = true;
            if (ref.start_period + ref.duration > periods - edge) late = true;
          }
          if (early && late) {
            ApplyRule(shift_rule, cats, kCatAntiSplitShift,
                      shift_rule.weight, 1, m_.teachers[t].name, teacher_id,
                      true, static_cast<int>(d), 0, day_refs[d]);
          }
        }
      }
    }
    // few_days ("celebryta"): active days beyond the allowed param (param 0
    // = every active day costs). Counted after the day loop.
    const ResolvedRule few_rule = teacher_rule("few_days");
    if (few_rule.mode != RuleMode::kOff) {
      const int64_t extra = static_cast<int64_t>(active_days_of_teacher_[t]) -
        static_cast<int64_t>(few_rule.param);
      if (extra > 0) {
        ApplyRule(few_rule, cats, kCatFewDays, few_rule.weight * extra,
                  static_cast<uint32_t>(extra), m_.teachers[t].name,
                  teacher_id, true, -1, 0);
      }
    }
  }
}

}  // namespace scoring
}  // namespace arrango
