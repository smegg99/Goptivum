// src/score/scorer_info.cc

#include <algorithm>
#include <map>
#include <vector>

#include "score/metrics.h"
#include "score/penalty_defs.h"
#include "score/scorer_internal.h"

namespace arrango {
namespace scoring {

// Hygiene detectors (INFO tier): polish signals a planner may care about
// but that never gate a pristine verdict — reported with located issues,
// zero penalty, zero effect on the objective, the metrics, or the tiers.
void Scorer::ScoreInfo() {
  const size_t nd = m_.days.size();
  // First/last occupied period per (division, day), union over participants
  // (a group lesson counts for its division — the day's shape as students
  // experience it).
  constexpr int kNone = -1;
  std::vector<std::vector<int>> first(m_.divisions.size(),
                                      std::vector<int>(nd, kNone));
  std::vector<std::vector<int>> last(m_.divisions.size(),
                                     std::vector<int>(nd, kNone));
  for (const Placed& p : placed_) {
    for (const Participant& part : m_.lessons[p.lesson_idx].participants) {
      int c = ix_.DivisionIdx(part.division_id);
      if (c < 0) continue;
      int& f = first[c][p.day_idx];
      int& l = last[c][p.day_idx];
      if (f == kNone || static_cast<int>(p.start) < f) {
        f = static_cast<int>(p.start);
      }
      const int end = static_cast<int>(p.start + p.duration - 1);
      if (l == kNone || end > l) l = end;
    }
  }

  auto info = [&] (const char* category, std::string label, Id division_id,
    int day_idx, uint32_t period, uint32_t count) {
    SoftIssue issue{ category, std::move(label), division_id, false,
                    day_idx >= 0 ? m_.days[day_idx].id : kNoId, period,
                    count, 0, {} };
    issue.locus.entities.push_back({ EntityKind::kDivision, division_id });
    info_issues_.push_back(std::move(issue));
    };

  for (size_t c = 0; c < m_.divisions.size(); ++c) {
    int earliest = kNone, latest = kNone;
    for (size_t d = 0; d < nd; ++d) {
      if (first[c][d] == kNone) continue;  // no lessons that day
      if (earliest == kNone || first[c][d] < earliest) earliest = first[c][d];
      if (latest == kNone || first[c][d] > latest) latest = first[c][d];
      // A day starting later than period kInfoLatestFirstLesson.
      if (first[c][d] > static_cast<int>(kInfoLatestFirstLesson)) {
        info(kCatLateFirstLesson, m_.divisions[c].name, m_.divisions[c].id,
             static_cast<int>(d), static_cast<uint32_t>(first[c][d]), 1);
      }
    }
    // Start times drifting more than the tolerance across the week.
    if (earliest != kNone &&
      latest - earliest > static_cast<int>(kInfoStartVarianceMax)) {
      info(kCatStartVariance, m_.divisions[c].name, m_.divisions[c].id, -1,
           static_cast<uint32_t>(earliest),
           static_cast<uint32_t>(latest - earliest));
    }
  }

  // A subject that only ever sits in the division's last daily slot.
  std::map<std::pair<int, Id>, std::vector<const Placed*>> by_div_subject;
  for (const Placed& p : placed_) {
    const LessonInstance& l = m_.lessons[p.lesson_idx];
    for (const Participant& part : l.participants) {
      int c = ix_.DivisionIdx(part.division_id);
      if (c >= 0) by_div_subject[{c, l.subject_id}].push_back(&p);
    }
  }
  for (const auto& [key, placements] : by_div_subject) {
    if (placements.size() < 2) continue;
    const auto [c, subject_id] = key;
    const bool always_last = std::all_of(
        placements.begin(), placements.end(), [&] (const Placed* p) {
          return static_cast<int>(p->start + p->duration - 1) ==
                 last[c][p->day_idx];
        });
    if (!always_last) continue;
    int subject = ix_.SubjectIdx(subject_id);
    info(kCatSubjectAlwaysLast,
         m_.divisions[c].name + " · " +
             (subject >= 0 ? m_.subjects[subject].name : "?"),
         m_.divisions[c].id, -1, 0,
         static_cast<uint32_t>(placements.size()));
  }
}

}  // namespace scoring
}  // namespace arrango
