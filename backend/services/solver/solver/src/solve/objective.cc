// src/solve/objective.cc

#include "solve/objective.h"

#include <algorithm>
#include <map>
#include <vector>

#include "score/penalty_defs.h"
#include "solve/objective_internal.h"

namespace arrango {
namespace objective_detail {

Builder::Builder(const SchoolModel& m, const ModelIndex& ix,
                 const CandidateSet& cs, const std::vector<sat::BoolVar>& x,
                 sat::CpModelBuilder& cp, const RuleResolver& rules)
    : m_(m),
      ix_(ix),
      w_(WithDefaults(m.weights)),
      cs_(cs),
      x_(x),
      cp_(cp),
      rules_(rules),
      atoms_(BuildAtoms(m, ix)),
      streams_(BuildStudentStreams(m, ix, atoms_)) {
  // Stream = atom; a lesson occupies the streams whose atom it covers.
  streams_of_lesson_.resize(m_.lessons.size());
  for (size_t li = 0; li < m_.lessons.size(); ++li) {
    streams_of_lesson_[li] = atoms_.of_lesson[li];
  }
  // Invert once: candidate -> its streams and its teacher.
  candidates_of_stream_.assign(streams_.size(), {});
  candidates_of_teacher_.assign(m_.teachers.size(), {});
  for (size_t ci = 0; ci < cs_.all.size(); ++ci) {
    const Candidate& c = cs_.all[ci];
    for (int s : streams_of_lesson_[c.lesson_idx]) {
      candidates_of_stream_[s].push_back(static_cast<int>(ci));
    }
    const LessonInstance& l = m_.lessons[c.lesson_idx];
    if (l.requires_teacher) {
      int t = ix_.TeacherIdx(l.teacher_id);
      if (t >= 0) candidates_of_teacher_[t].push_back(static_cast<int>(ci));
    }
  }
}

void Builder::Build() {
  // Only soft penalties live here; hard daily-load/parity constraints are added
  // by AddHardConstraints (see solve/constraints.cc).
  PerCandidateCosts();
  StreamTerms();
  TeacherTerms();
  cp_.Minimize(objective_);
}

uint32_t Builder::LessonPriorityOf(const LessonInstance& l) const {
  return LessonPriority(m_, ix_, l);
}

// Late lessons, stability, prefer-early: pure per-candidate costs.
void Builder::PerCandidateCosts() {
  // Priority, preference matching, and rule resolution are per-LESSON
  // facts; resolve them once instead of per candidate (the candidate loop
  // dominates on large models). Weights stay per-preference so the
  // rounding matches the scorer exactly.
  std::vector<uint32_t> priority_of(m_.lessons.size());
  std::vector<std::vector<int64_t>> prefer_early_of(m_.lessons.size());
  // late_student is division- and subject-scoped: group each lesson's
  // streams by division so the per-stream rounding matches the scorer.
  // Only SOFT groups cost anything (hard prunes candidates; off ignores).
  struct LateGroup { int64_t streams; int64_t base; };
  std::vector<std::vector<LateGroup>> late_groups(m_.lessons.size());
  std::vector<int64_t> late_teacher_base(m_.lessons.size(), 0);
  std::vector<int64_t> stability_base(m_.lessons.size(), 0);
  std::vector<bool> early_on(m_.lessons.size(), true);
  for (size_t li = 0; li < m_.lessons.size(); ++li) {
    const LessonInstance& l = m_.lessons[li];
    priority_of[li] = LessonPriorityOf(m_.lessons[li]);

    std::map<int, int64_t> streams_per_division;
    for (int s : streams_of_lesson_[li]) {
      ++streams_per_division[streams_[s].division_idx];
    }
    for (const auto& [d, n] : streams_per_division) {
      const ResolvedRule r =
          rules_.For("late_student", m_.divisions[d].year_id,
                     m_.divisions[d].id, l.subject_id, kNoId);
      if (r.mode == RuleMode::kSoft || r.mode == RuleMode::kDefault) {
        late_groups[li].push_back(
            { n, r.weight > 0 ? r.weight : w_.late_student_lesson_base });
      }
    }
    if (l.requires_teacher) {
      const ResolvedRule r =
          rules_.For("late_teacher", kNoId, kNoId, kNoId, l.teacher_id);
      if (r.mode == RuleMode::kSoft || r.mode == RuleMode::kDefault) {
        late_teacher_base[li] =
            r.weight > 0 ? r.weight : w_.late_teacher_finish_base;
      }
    }
    // Rules scoped like the scorer's ScoreLessonCosts: the lesson's FIRST
    // participant's division.
    const int c0 = l.participants.empty()
                       ? -1
                       : ix_.DivisionIdx(l.participants.front().division_id);
    if (c0 >= 0) {
      const ResolvedRule st = rules_.For(
          "stability", m_.divisions[c0].year_id, m_.divisions[c0].id, kNoId,
          kNoId);
      if (st.mode != RuleMode::kOff) {
        stability_base[li] =
            st.weight > 0 ? st.weight : w_.stability_move_base;
      }
      early_on[li] =
          rules_.For("prefer_early", m_.divisions[c0].year_id,
                     m_.divisions[c0].id, l.subject_id, kNoId).mode !=
          RuleMode::kOff;
    }
    if (!early_on[li]) continue;
    for (const Preference& pref : m_.preferences) {
      if (pref.kind != PreferenceKind::kPreferEarly) continue;
      if (!PrefMatchesLesson(m_, ix_, pref, m_.lessons[li])) continue;
      prefer_early_of[li].push_back(pref.weight);
    }
  }

  for (size_t ci = 0; ci < cs_.all.size(); ++ci) {
    const Candidate& c = cs_.all[ci];
    const LessonInstance& l = m_.lessons[c.lesson_idx];
    const uint32_t priority = priority_of[c.lesson_idx];
    int64_t cost = 0;

    for (uint32_t q = c.start; q < c.start + l.duration; ++q) {
      for (const LateGroup& g : late_groups[c.lesson_idx]) {
        cost += g.streams *
                StudentWeight(LatePeriodCost(g.base, q,
                                             w_.late_threshold_period),
                              priority);
      }
      if (late_teacher_base[c.lesson_idx] > 0) {
        cost += LatePeriodCost(late_teacher_base[c.lesson_idx], q,
                               w_.late_threshold_period);
      }
    }
    if (early_on[c.lesson_idx] && w_.early_start_base > 0) {
      cost += StudentWeight(w_.early_start_base * c.start, priority);
    }
    if (stability_base[c.lesson_idx] > 0 && l.previous_placement) {
      Placement placed{ m_.days[c.day_idx].id, c.start,
                       c.room_idx >= 0 ? m_.rooms[c.room_idx].id : kNoId };
      if (!(placed == *l.previous_placement)) {
        cost += stability_base[c.lesson_idx];
      }
    }
    for (int64_t weight : prefer_early_of[c.lesson_idx]) {
      cost += StudentWeight(weight * c.start, priority);
    }
    if (cost != 0) objective_ += cost * x_[ci];
  }
}

const EffectiveDailyLoad& Builder::DailyLoadOf(int division_idx) {
  if (daily_load_cache_.empty()) {
    daily_load_cache_.resize(m_.divisions.size());
  }
  std::optional<EffectiveDailyLoad>& slot = daily_load_cache_[division_idx];
  if (!slot) slot = ResolveDailyLoad(m_, ix_, atoms_, division_idx);
  return *slot;
}

}  // namespace objective_detail

void AddSoftObjective(
    const SchoolModel& m, const ModelIndex& ix, const CandidateSet& cs,
    const std::vector<operations_research::sat::BoolVar>& x,
    operations_research::sat::CpModelBuilder& cp, const RuleResolver& rules) {
  objective_detail::Builder(m, ix, cs, x, cp, rules).Build();
}

}  // namespace arrango
