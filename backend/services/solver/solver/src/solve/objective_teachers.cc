// src/solve/objective_teachers.cc

#include <map>
#include <vector>

#include "score/penalty_defs.h"
#include "solve/objective_internal.h"

namespace arrango {
namespace objective_detail {
namespace {

// Every teacher rule resolved once, with its "does this term do anything"
// flag — per-teacher overrides are Dobry Plan's comfort flags as data (same
// scoping as the scorer).
struct TeacherRuleSet {
  ResolvedRule gap;
  ResolvedRule room;
  ResolvedRule lonely;
  ResolvedRule shift;
  ResolvedRule daily;
  ResolvedRule few;
  int64_t lonely_weight{ 0 };
  bool want_lonely{ false };
  bool want_shift{ false };
  bool want_daily{ false };
  bool want_few{ false };

  bool AnyActive() const {
    return gap.mode != RuleMode::kOff || room.mode != RuleMode::kOff ||
      want_lonely || want_shift || want_daily || want_few;
  }
};

TeacherRuleSet ResolveTeacherRules(const RuleResolver& rules, Id teacher_id,
                                   const Weights& w) {
  TeacherRuleSet tr;
  tr.gap = rules.For("teacher_gaps", kNoId, kNoId, kNoId, teacher_id);
  tr.room = rules.For("room_changes", kNoId, kNoId, kNoId, teacher_id);
  tr.lonely = rules.For("single_lesson_day", kNoId, kNoId, kNoId, teacher_id);
  tr.shift = rules.For("anti_split_shift", kNoId, kNoId, kNoId, teacher_id);
  tr.daily = rules.For("teach_daily", kNoId, kNoId, kNoId, teacher_id);
  tr.few = rules.For("few_days", kNoId, kNoId, kNoId, teacher_id);
  // single_lesson_day prices lonely days only when a weight is set (its
  // default weight is 0 = observe-and-warn only, no search pressure).
  tr.lonely_weight = tr.lonely.mode == RuleMode::kSoft
      ? (tr.lonely.weight > 0 ? tr.lonely.weight : w.single_lesson_day_base)
      : 0;
  tr.want_lonely = tr.lonely.mode == RuleMode::kHard || tr.lonely_weight > 0;
  tr.want_shift = tr.shift.mode == RuleMode::kHard ||
      (tr.shift.mode == RuleMode::kSoft && tr.shift.weight > 0);
  tr.want_daily = tr.daily.mode == RuleMode::kHard ||
      (tr.daily.mode == RuleMode::kSoft && tr.daily.weight > 0);
  tr.want_few = tr.few.mode == RuleMode::kHard ||
      (tr.few.mode == RuleMode::kSoft && tr.few.weight > 0);
  return tr;
}

// Exactly-channeled OR: the returned var is 1 iff any input var is 1.
sat::BoolVar OrOf(const std::vector<sat::BoolVar>& vars,
                  sat::CpModelBuilder& cp) {
  sat::BoolVar b = cp.NewBoolVar();
  sat::LinearExpr sum;
  for (const sat::BoolVar& v : vars) {
    cp.AddImplication(v, b);
    sum += v;
  }
  cp.AddLessOrEqual(b, sum);
  return b;
}

// One teacher's candidates regrouped per day: occupancy covers, external
// blocks, room usage, per-day lesson counts, and the edge-period vars the
// anti-split-shift predicate needs.
struct TeacherDays {
  std::vector<std::vector<std::vector<sat::BoolVar>>> covers;
  std::vector<std::vector<bool>> blocked;
  // room_idx -> covering x vars; roomed_vars carries every roomed candidate
  // so a roomless-only day contributes 0, never a negative term.
  std::vector<std::map<int, std::vector<sat::BoolVar>>> rooms;
  std::vector<std::vector<sat::BoolVar>> roomed_vars;
  // One candidate wins per lesson, so summing a day's candidate vars counts
  // placed lessons.
  std::vector<sat::LinearExpr> lesson_count;
  std::vector<int> lessons_possible;
  // Edge-period membership per candidate — same start-period test the
  // scorer's anti-split-shift predicate uses.
  std::vector<std::vector<sat::BoolVar>> early_vars;
  std::vector<std::vector<sat::BoolVar>> late_vars;
  // x vars per day, for the activity (teach_daily / few_days) ORs.
  std::vector<std::vector<sat::BoolVar>> day_vars;
};

TeacherDays CollectTeacherDays(const SchoolModel& m, const ModelIndex& ix,
    const CandidateSet& cs, const std::vector<sat::BoolVar>& x,
    const std::vector<int>& teacher_candidates, size_t t,
    const TeacherRuleSet& tr) {
  TeacherDays days;
  const size_t nd = m.days.size();
  days.covers.resize(nd);
  days.blocked.resize(nd);
  for (size_t d = 0; d < nd; ++d) {
    days.covers[d].resize(m.days[d].period_count);
    days.blocked[d].assign(m.days[d].period_count, false);
  }
  for (const ExternalBlock& blk : m.external_blocks) {
    if (blk.target != BlockTarget::kTeacher ||
        ix.TeacherIdx(blk.target_id) != static_cast<int>(t)) {
      continue;
    }
    int d = ix.DayIdx(blk.day_id);
    if (d < 0) continue;
    for (uint32_t q = blk.start_period;
         q < blk.start_period + blk.duration && q < m.days[d].period_count;
         ++q) {
      days.blocked[d][q] = true;
    }
  }
  days.rooms.resize(nd);
  days.roomed_vars.resize(nd);
  days.lesson_count.resize(nd);
  days.lessons_possible.assign(nd, 0);
  days.early_vars.resize(nd);
  days.late_vars.resize(nd);
  days.day_vars.resize(nd);
  for (int ci : teacher_candidates) {
    const Candidate& c = cs.all[ci];
    const LessonInstance& l = m.lessons[c.lesson_idx];
    for (uint32_t q = c.start; q < c.start + l.duration; ++q) {
      days.covers[c.day_idx][q].push_back(x[ci]);
    }
    if (c.room_idx >= 0) {
      days.rooms[c.day_idx][c.room_idx].push_back(x[ci]);
      days.roomed_vars[c.day_idx].push_back(x[ci]);
    }
    days.lesson_count[c.day_idx] += x[ci];
    ++days.lessons_possible[c.day_idx];
    days.day_vars[c.day_idx].push_back(x[ci]);
    if (tr.want_shift) {
      const uint32_t edge = tr.shift.param > 0
          ? tr.shift.param : kAntiSplitShiftDefaultEdge;
      const uint32_t periods = m.days[c.day_idx].period_count;
      if (periods > 2 * edge) {
        if (c.start < edge) days.early_vars[c.day_idx].push_back(x[ci]);
        if (c.start + l.duration > periods - edge) {
          days.late_vars[c.day_idx].push_back(x[ci]);
        }
      }
    }
  }
  return days;
}

// Room changes on one day: costs (distinct rooms used - 1) so hopping
// between rooms within a day is discouraged; HARD allows one room at most.
void AddRoomChangeDay(const std::map<int, std::vector<sat::BoolVar>>& rooms,
    const std::vector<sat::BoolVar>& roomed_vars, const ResolvedRule& rule,
    const Weights& w, sat::CpModelBuilder& cp, sat::LinearExpr& objective) {
  if (rule.mode == RuleMode::kOff || rooms.size() < 2) return;
  sat::LinearExpr used_sum;
  for (auto& [room_idx, vars] : rooms) {
    sat::BoolVar used = cp.NewBoolVar();
    sat::LinearExpr sum;
    for (const sat::BoolVar& v : vars) {
      cp.AddImplication(v, used);
      sum += v;
    }
    cp.AddLessOrEqual(used, sum);  // used == OR(vars), exact
    used_sum += used;
  }
  sat::BoolVar active = cp.NewBoolVar();
  sat::LinearExpr any;
  for (const sat::BoolVar& v : roomed_vars) {
    any += v;
    cp.AddImplication(v, active);  // active == OR(vars), exact
  }
  cp.AddLessOrEqual(active, any);
  if (rule.mode == RuleMode::kHard) {
    // HARD: at most one room per teacher-day.
    cp.AddLessOrEqual(used_sum - active, 0);
  }
  else {
    // Non-negative channeling (see AddRunTerm).
    sat::IntVar changes =
        cp.NewIntVar({ 0, static_cast<int64_t>(rooms.size()) - 1 });
    cp.AddEquality(changes, used_sum - active);
    objective += (rule.weight > 0 ? rule.weight : w.room_change_base) * changes;
  }
}

// single_lesson_day: lonely == (exactly one lesson placed this day).
void AddLonelyDay(const sat::LinearExpr& lesson_count, int lessons_possible,
    const TeacherRuleSet& tr, sat::CpModelBuilder& cp,
    sat::LinearExpr& objective) {
  if (!tr.want_lonely || lessons_possible < 1) return;
  if (tr.lonely.mode == RuleMode::kHard) {
    cp.AddNotEqual(lesson_count, 1);
  }
  else {
    sat::BoolVar lonely = cp.NewBoolVar();
    cp.AddEquality(lesson_count, 1).OnlyEnforceIf(lonely);
    cp.AddNotEqual(lesson_count, 1).OnlyEnforceIf(lonely.Not());
    objective += tr.lonely_weight * lonely;
  }
}

// anti_split_shift on one day: no lessons in BOTH the first and last `edge`
// periods (E and L are exactly-channeled ORs).
void AddAntiSplitShiftDay(const std::vector<sat::BoolVar>& early_vars,
    const std::vector<sat::BoolVar>& late_vars, const ResolvedRule& rule,
    sat::CpModelBuilder& cp, sat::LinearExpr& objective) {
  if (early_vars.empty() || late_vars.empty()) return;
  const sat::BoolVar early = OrOf(early_vars, cp);
  const sat::BoolVar late = OrOf(late_vars, cp);
  if (rule.mode == RuleMode::kHard) {
    cp.AddLessOrEqual(sat::LinearExpr(early) + late, 1);
  }
  else {
    sat::BoolVar both = cp.NewBoolVar();
    cp.AddGreaterOrEqual(both, sat::LinearExpr(early) + late - 1);
    cp.AddImplication(both, early);
    cp.AddImplication(both, late);
    objective += rule.weight * both;
  }
}

}  // namespace

// Teacher gaps (capped), room changes, lonely days, and the activity-shape
// rules (teach_daily, few_days, anti_split_shift). A teacher's occupancy is
// the union of their lessons.
void Builder::TeacherTerms() {
  // Teacher days have no window rule — only the gap exit applies.
  static const ResolvedRule kNoWindows{ RuleMode::kOff, 0, 0 };
  for (size_t t = 0; t < m_.teachers.size(); ++t) {
    const TeacherRuleSet tr =
        ResolveTeacherRules(rules_, m_.teachers[t].id, w_);
    if (!tr.AnyActive()) continue;
    const TeacherDays days =
        CollectTeacherDays(m_, ix_, cs_, x_, candidates_of_teacher_[t], t, tr);

    sat::LinearExpr activity_sum;  // active days, for few_days
    int days_with_periods = 0;
    for (size_t d = 0; d < m_.days.size(); ++d) {
      AddGapTerm(MakeCells(m_.days[d].period_count, days.covers[d],
                           days.blocked[d]),
                 tr.gap.weight > 0 ? tr.gap.weight : w_.teacher_gap_base,
                 /*capped=*/true, tr.gap, kNoWindows, 0);
      AddRoomChangeDay(days.rooms[d], days.roomed_vars[d], tr.room, w_, cp_,
                       objective_);
      AddLonelyDay(days.lesson_count[d], days.lessons_possible[d], tr, cp_,
                   objective_);
      // teach_daily ("codziennie"): the teacher must be active every day
      // with periods. A candidate-less day can never be active: hard goes
      // infeasible; soft pays the weight as a constant — exactly the
      // scorer's empty-day count. The activity OR is shared with few_days.
      if ((tr.want_daily || tr.want_few) && m_.days[d].period_count >= 1) {
        if (!days.day_vars[d].empty()) {
          const sat::BoolVar act = OrOf(days.day_vars[d], cp_);
          activity_sum += act;
          if (tr.want_daily) {
            if (tr.daily.mode == RuleMode::kHard) {
              cp_.AddBoolOr(days.day_vars[d]);
            }
            else {
              objective_ += tr.daily.weight * act.Not();
            }
          }
        }
        else if (tr.want_daily) {
          if (tr.daily.mode == RuleMode::kHard) {
            cp_.AddLessOrEqual(sat::LinearExpr(2), 1);  // infeasible
          }
          else {
            objective_ += tr.daily.weight;  // unavoidable empty day
          }
        }
        ++days_with_periods;
      }
      if (tr.want_shift) {
        AddAntiSplitShiftDay(days.early_vars[d], days.late_vars[d], tr.shift,
                             cp_, objective_);
      }
    }
    // few_days ("celebryta"): active days beyond param. activity_sum is
    // exactly-channeled, so extra == the scorer's max(0, active - param).
    if (tr.want_few && days_with_periods > 0) {
      const int64_t param = static_cast<int64_t>(tr.few.param);
      if (tr.few.mode == RuleMode::kHard) {
        cp_.AddLessOrEqual(activity_sum, param);
      }
      else if (days_with_periods > param) {
        sat::IntVar extra =
            cp_.NewIntVar({ 0, days_with_periods - param });
        cp_.AddMaxEquality(extra, std::vector<sat::LinearExpr>{
            activity_sum - param, sat::LinearExpr(0)});
        objective_ += tr.few.weight * extra;
      }
    }
  }
}

}  // namespace objective_detail
}  // namespace arrango
