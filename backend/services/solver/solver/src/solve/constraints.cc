// src/solve/constraints.cc

#include "solve/constraints.h"

#include <algorithm>
#include <map>
#include <set>
#include <utility>
#include <vector>

#include "model/atoms.h"
#include "model/daily_load.h"
#include "score/penalty_defs.h"

namespace arrango {
  namespace {

    namespace sat = operations_research::sat;

    // Exact OR of booleans into a fresh var: r == (any v is true).
    sat::BoolVar OrVar(const std::vector<sat::BoolVar>& vars,
      sat::CpModelBuilder& cp) {
      sat::BoolVar r = cp.NewBoolVar();
      sat::LinearExpr sum;
      for (const sat::BoolVar& v : vars) {
        cp.AddImplication(v, r);  // v => r
        sum += v;
      }
      cp.AddLessOrEqual(r, sum);  // r => some v
      return r;
    }

    // (entity, day, period) -> the candidate vars that occupy that slot.
    class OccupancyBuckets {
    public:
      OccupancyBuckets(size_t entities, size_t days, size_t periods)
        : days_(days), periods_(periods), buckets_(entities* days* periods) {
      }

      std::vector<sat::BoolVar>& At(int entity, int day, uint32_t period) {
        return buckets_[(static_cast<size_t>(entity) * days_ + day) * periods_ +
          period];
      }
      size_t size() const { return buckets_.size(); }
      std::vector<sat::BoolVar>& Flat(size_t i) { return buckets_[i]; }

    private:
      size_t days_, periods_;
      std::vector<std::vector<sat::BoolVar>> buckets_;
    };

    // At most one candidate may occupy any single slot of an entity.
    void AtMostOnePerSlot(OccupancyBuckets& b, sat::CpModelBuilder& cp) {
      for (size_t i = 0; i < b.size(); ++i) {
        if (b.Flat(i).size() >= 2) cp.AddAtMostOne(b.Flat(i));
      }
    }

    // Every lesson takes exactly one of its candidates. Under explain gates
    // the "at least one" half is enforced per DIVISION switch, so a core
    // can say "dropping 3B's lessons makes the rest solvable".
    void AddExactlyOnePerLesson(const SchoolModel& m, const CandidateSet& cs,
      const std::vector<sat::BoolVar>& x,
      sat::CpModelBuilder& cp, const ExplainGates* gates) {
      for (size_t li = 0; li < cs.by_lesson.size(); ++li) {
        std::vector<sat::BoolVar> vars;
        for (int ci : cs.by_lesson[li]) vars.push_back(x[ci]);
        const sat::BoolVar* gate = nullptr;
        if (gates != nullptr && !m.lessons[li].participants.empty()) {
          auto it = gates->completeness_of_division.find(
            m.lessons[li].participants.front().division_id);
          if (it != gates->completeness_of_division.end()) gate = &it->second;
        }
        if (gate == nullptr) {
          cp.AddExactlyOne(vars);
        }
        else {
          cp.AddAtMostOne(vars);
          cp.AddBoolOr(vars).OnlyEnforceIf(*gate);
        }
      }
    }

    // No teacher/room/student-atom is double-booked in any slot. Student occupancy
    // is per ATOM: two lessons whose student sets intersect share an atom, so
    // at-most-one over each atom's slot forbids exactly the real overlaps.
    void AddNoOverlap(const SchoolModel& m, const ModelIndex& ix,
      const CandidateSet& cs, const AtomSet& atoms,
      const std::vector<sat::BoolVar>& x, sat::CpModelBuilder& cp) {
      const size_t days = m.days.size();
      const size_t periods = m.periods.size();
      OccupancyBuckets teacher_b(m.teachers.size(), days, periods);
      OccupancyBuckets room_b(m.rooms.size(), days, periods);
      OccupancyBuckets atom_b(atoms.atoms.size(), days, periods);

      for (size_t ci = 0; ci < cs.all.size(); ++ci) {
        const Candidate& c = cs.all[ci];
        const LessonInstance& l = m.lessons[c.lesson_idx];
        const int t = l.requires_teacher ? ix.TeacherIdx(l.teacher_id) : -1;
        for (uint32_t q = c.start; q < c.start + l.duration; ++q) {
          if (t >= 0) teacher_b.At(t, c.day_idx, q).push_back(x[ci]);
          if (c.room_idx >= 0) room_b.At(c.room_idx, c.day_idx, q).push_back(x[ci]);
          for (int a : atoms.of_lesson[c.lesson_idx]) {
            atom_b.At(a, c.day_idx, q).push_back(x[ci]);
          }
        }
      }

      AtMostOnePerSlot(teacher_b, cp);  // teacher teaches one lesson at a time
      AtMostOnePerSlot(room_b, cp);     // room hosts one lesson at a time
      AtMostOnePerSlot(atom_b, cp);     // students attend one lesson at a time
    }

    // Lessons of one parallel block share a (day, start) slot, channeled through a
    // per-slot y var that is 1 iff every block member is placed there.
    void AddParallelBlocks(const ModelIndex& ix, const CandidateSet& cs,
      const std::vector<sat::BoolVar>& x,
      sat::CpModelBuilder& cp) {
      for (const auto& block : ix.ParallelBlocks()) {
        std::map<std::pair<int, uint32_t>, sat::BoolVar> slots;
        for (int li : block) {
          for (int ci : cs.by_lesson[li]) {
            const Candidate& c = cs.all[ci];
            auto key = std::make_pair(c.day_idx, c.start);
            if (!slots.contains(key)) slots.emplace(key, cp.NewBoolVar());
          }
        }
        std::vector<sat::BoolVar> ys;
        for (auto& [key, y] : slots) ys.push_back(y);
        cp.AddExactlyOne(ys);
        for (int li : block) {
          std::map<std::pair<int, uint32_t>, std::vector<sat::BoolVar>> per_slot;
          for (int ci : cs.by_lesson[li]) {
            const Candidate& c = cs.all[ci];
            per_slot[{c.day_idx, c.start}].push_back(x[ci]);
          }
          for (auto& [key, y] : slots) {
            auto it = per_slot.find(key);
            sat::LinearExpr sum;
            if (it != per_slot.end()) {
              for (const sat::BoolVar& v : it->second) sum += v;
            }
            cp.AddEquality(sum, y);  // no candidate at slot => y forced to 0
          }
        }
      }
    }

    // HARD daily-load rule at the WHOLE-DIVISION level: on each active day the
    // division is in session for at least its (auto-relaxed) minimum and at most
    // its maximum periods — a period counts once even when groups run parallel
    // splits (union occupancy). Uses the same ResolveDailyLoad the validator
    // reports against.
    //
    // Per-group attendance PARITY (all a division's groups attend the same days)
    // was intentionally removed: real vocational timetables legitimately run
    // different groups on different days, so parity made real imports invalid.
    void AddDailyLoadConstraints(const SchoolModel& m, const ModelIndex& ix,
      const CandidateSet& cs, const AtomSet& atoms,
      const std::vector<sat::BoolVar>& x,
      sat::CpModelBuilder& cp, const ExplainGates* gates) {
      int64_t active_days = 0;
      for (const Day& day : m.days) {
        if (day.period_count >= 1) ++active_days;
      }

      // Unique division indices per lesson (a lesson touches each once).
      std::vector<std::vector<int>> divisions_of_lesson(m.lessons.size());
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        std::vector<int>& out = divisions_of_lesson[li];
        for (const Participant& p : m.lessons[li].participants) {
          int c = ix.DivisionIdx(p.division_id);
          if (c >= 0 && std::find(out.begin(), out.end(), c) == out.end()) {
            out.push_back(c);
          }
        }
      }

      // div_period[division][day][period] = candidate vars covering that
      // slot for the division; the OR of each bucket says "the division is
      // in session here" (parallel splits collapse into one occupied period).
      const size_t nd = m.days.size();
      std::vector<std::vector<std::vector<std::vector<sat::BoolVar>>>> div_period(
        m.divisions.size());
      for (size_t c = 0; c < m.divisions.size(); ++c) {
        div_period[c].resize(nd);
        for (size_t d = 0; d < nd; ++d) {
          div_period[c][d].resize(m.days[d].period_count);
        }
      }

      for (size_t ci = 0; ci < cs.all.size(); ++ci) {
        const Candidate& cand = cs.all[ci];
        const LessonInstance& l = m.lessons[cand.lesson_idx];
        for (int c : divisions_of_lesson[cand.lesson_idx]) {
          for (uint32_t q = cand.start; q < cand.start + l.duration; ++q) {
            div_period[c][cand.day_idx][q].push_back(x[ci]);
          }
        }
      }

      for (size_t c = 0; c < m.divisions.size(); ++c) {
        const EffectiveDailyLoad eff =
          ResolveDailyLoad(m, ix, atoms, static_cast<int>(c));
        // Division weekly (sum of its lessons' periods) for the max fallback,
        // via the prebuilt index instead of rescanning every lesson.
        int64_t weekly = 0;
        for (int li : ix.LessonsOfDivision(static_cast<int>(c))) {
          weekly += m.lessons[li].duration;
        }
        const int64_t lo = eff.min_per_day;
        const int64_t hi = eff.max_per_day != 0
          ? static_cast<int64_t>(eff.max_per_day)
          : DayLoadMax(weekly, active_days);

        for (size_t d = 0; d < nd; ++d) {
          if (m.days[d].period_count < 1) continue;  // inactive day
          // Union occupancy count in [lo, hi].
          sat::LinearExpr occupied;
          for (uint32_t q = 0; q < m.days[d].period_count; ++q) {
            const std::vector<sat::BoolVar>& vs = div_period[c][d][q];
            if (vs.empty()) continue;
            occupied += OrVar(vs, cp);
          }
          const sat::BoolVar* gate = nullptr;
          if (gates != nullptr) {
            auto it = gates->bounds_of_division.find(m.divisions[c].id);
            if (it != gates->bounds_of_division.end()) gate = &it->second;
          }
          if (gate == nullptr) {
            cp.AddLinearConstraint(occupied,
              operations_research::Domain(lo, hi));
          }
          else {
            cp.AddLinearConstraint(occupied,
              operations_research::Domain(lo, hi)).OnlyEnforceIf(*gate);
          }
        }
      }
    }

    // Relative-placement links (SAME_DAY / DIFFERENT_DAY / CONSECUTIVE) —
    // hard by design. Per linked lesson: exact per-day indicator booleans
    // (a lesson is placed exactly once, so at most one indicator is 1);
    // the kind constrains the indicators, and CONSECUTIVE adds pairwise
    // forbids of non-adjacent same-day candidate combos (linked lessons
    // are few, so the pairwise cost stays negligible).
    void AddLessonLinks(const SchoolModel& m, const ModelIndex& ix,
      const CandidateSet& cs, const std::vector<sat::BoolVar>& x,
      sat::CpModelBuilder& cp) {
      if (m.lesson_links.empty()) return;
      std::map<int, std::vector<sat::BoolVar>> indicators_of;
      auto indicators = [&] (int li) -> const std::vector<sat::BoolVar>& {
        auto it = indicators_of.find(li);
        if (it != indicators_of.end()) return it->second;
        std::vector<std::vector<sat::BoolVar>> per_day(m.days.size());
        for (int ci : cs.by_lesson[li]) {
          per_day[cs.all[ci].day_idx].push_back(x[ci]);
        }
        std::vector<sat::BoolVar> ind;
        for (size_t d = 0; d < m.days.size(); ++d) {
          sat::BoolVar b = cp.NewBoolVar();
          if (per_day[d].empty()) {
            cp.AddEquality(sat::LinearExpr(b), 0);
          }
          else {
            sat::LinearExpr sum;
            for (const sat::BoolVar& v : per_day[d]) {
              cp.AddImplication(v, b);
              sum += v;
            }
            cp.AddLessOrEqual(b, sum);  // b == OR(day candidates), exact
          }
          ind.push_back(b);
        }
        return indicators_of.emplace(li, std::move(ind)).first->second;
        };

      for (const LessonLink& link : m.lesson_links) {
        std::vector<int> members;
        for (Id id : link.lesson_ids) {
          int li = ix.LessonIdx(id);
          if (li >= 0) members.push_back(li);
        }
        if (members.size() < 2) continue;  // preflight already refused

        if (link.kind == LessonLinkKind::kSameDay ||
          link.kind == LessonLinkKind::kConsecutive) {
          const std::vector<sat::BoolVar>& anchor = indicators(members[0]);
          for (size_t i = 1; i < members.size(); ++i) {
            const std::vector<sat::BoolVar>& other = indicators(members[i]);
            for (size_t d = 0; d < m.days.size(); ++d) {
              cp.AddEquality(sat::LinearExpr(anchor[d]), other[d]);
            }
          }
        }
        else if (link.kind == LessonLinkKind::kDifferentDay) {
          for (size_t d = 0; d < m.days.size(); ++d) {
            sat::LinearExpr day_sum;
            for (int li : members) day_sum += indicators(li)[d];
            cp.AddLessOrEqual(day_sum, 1);
          }
        }

        if (link.kind == LessonLinkKind::kConsecutive) {
          // Adjacency between chain neighbors (list order when `ordered`,
          // either orientation otherwise; preflight limits unordered
          // consecutive links to 2 members).
          for (size_t i = 0; i + 1 < members.size(); ++i) {
            const int a = members[i], b = members[i + 1];
            for (int ca : cs.by_lesson[a]) {
              for (int cb : cs.by_lesson[b]) {
                const Candidate& pa = cs.all[ca];
                const Candidate& pb = cs.all[cb];
                if (pa.day_idx != pb.day_idx) continue;  // same-day handles
                const bool forward = pb.start ==
                  pa.start + m.lessons[a].duration;
                const bool backward = pa.start ==
                  pb.start + m.lessons[b].duration;
                const bool adjacent =
                  link.ordered ? forward : (forward || backward);
                if (!adjacent) {
                  cp.AddBoolOr({ x[ca].Not(), x[cb].Not() });
                }
              }
            }
          }
        }
      }
    }

    // Edge-of-day lessons must open/close the day for their STUDENTS. A
    // hard placement fact, so it lives HERE: the construct phase and every
    // LNS sub-model build hard constraints without the objective, and an
    // edge violation in a construct incumbent would poison every LNS trial
    // at the validity gate (that exact bug shipped once — see the
    // HardConstraintLayerAloneEnforcesEdgePlacement regression test).
    // Blocks don't count: only real lessons make something "not first",
    // matching validator CheckEdgePlacement.
    void AddEdgePlacement(const SchoolModel& m, const ModelIndex& ix,
      const CandidateSet& cs, const AtomSet& atoms,
      const std::vector<sat::BoolVar>& x, sat::CpModelBuilder& cp) {
      std::set<int> edge_atoms;
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        if (m.lessons[li].edge == EdgePlacement::kNone) continue;
        for (int a : atoms.of_lesson[li]) edge_atoms.insert(a);
      }
      if (edge_atoms.empty()) return;
      // Lesson covers per needed atom: [day][period] -> candidate vars.
      std::map<int, std::vector<std::vector<std::vector<sat::BoolVar>>>>
        covers_of_atom;
      for (int a : edge_atoms) {
        auto& grid = covers_of_atom[a];
        grid.resize(m.days.size());
        for (size_t d = 0; d < m.days.size(); ++d) {
          grid[d].resize(m.days[d].period_count);
        }
      }
      for (size_t ci = 0; ci < cs.all.size(); ++ci) {
        const Candidate& c = cs.all[ci];
        for (int a : atoms.of_lesson[c.lesson_idx]) {
          auto it = covers_of_atom.find(a);
          if (it == covers_of_atom.end()) continue;
          for (uint32_t q = c.start;
            q < c.start + m.lessons[c.lesson_idx].duration; ++q) {
            it->second[c.day_idx][q].push_back(x[ci]);
          }
        }
      }
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        const LessonInstance& l = m.lessons[li];
        if (l.edge == EdgePlacement::kNone) continue;
        for (int ci : cs.by_lesson[li]) {
          const Candidate& c = cs.all[ci];
          auto side_sum = [&] (bool before) {
            sat::LinearExpr sum;
            const uint32_t from = before ? 0 : c.start + l.duration;
            const uint32_t to =
              before ? c.start : m.days[c.day_idx].period_count;
            for (int a : atoms.of_lesson[li]) {
              const auto& grid = covers_of_atom.at(a);
              for (uint32_t q = from; q < to; ++q) {
                for (const sat::BoolVar& v : grid[c.day_idx][q]) sum += v;
              }
            }
            return sum;
            };
          if (l.edge == EdgePlacement::kFirst) {
            cp.AddEquality(side_sum(true), 0).OnlyEnforceIf(x[ci]);
          }
          else if (l.edge == EdgePlacement::kLast) {
            cp.AddEquality(side_sum(false), 0).OnlyEnforceIf(x[ci]);
          }
          else {  // kEither: pick a side per candidate, lesson-wide
            sat::BoolVar first_ok = cp.NewBoolVar();
            sat::BoolVar last_ok = cp.NewBoolVar();
            cp.AddBoolOr({ first_ok, last_ok }).OnlyEnforceIf(x[ci]);
            cp.AddEquality(side_sum(true), 0).OnlyEnforceIf(first_ok);
            cp.AddEquality(side_sum(false), 0).OnlyEnforceIf(last_ok);
          }
        }
      }
    }

  }  // namespace

  void AddHardConstraints(const SchoolModel& m, const ModelIndex& ix,
    const CandidateSet& cs,
    const std::vector<sat::BoolVar>& x,
    sat::CpModelBuilder& cp, const ExplainGates* gates) {
    const AtomSet atoms = BuildAtoms(m, ix);
    AddExactlyOnePerLesson(m, cs, x, cp, gates);
    AddNoOverlap(m, ix, cs, atoms, x, cp);
    AddParallelBlocks(ix, cs, x, cp);
    AddDailyLoadConstraints(m, ix, cs, atoms, x, cp, gates);
    AddLessonLinks(m, ix, cs, x, cp);
    AddEdgePlacement(m, ix, cs, atoms, x, cp);
  }

  void AddPlacementHints(const SchoolModel& m, const ModelIndex& ix,
    const CandidateSet& cs,
    const std::vector<sat::BoolVar>& x,
    sat::CpModelBuilder& cp) {
    // The full decision layer is hinted (chosen candidate 1, siblings 0); a
    // sparse hint would be dropped by CP-SAT's repair as incomplete.
    for (size_t li = 0; li < m.lessons.size(); ++li) {
      const auto& prev = m.lessons[li].previous_placement;
      if (!prev) continue;
      int day_idx = ix.DayIdx(prev->day_id);
      int room_idx = prev->room_id != kNoId ? ix.RoomIdx(prev->room_id) : -1;
      int chosen = -1;
      for (int ci : cs.by_lesson[li]) {
        const Candidate& c = cs.all[ci];
        if (c.day_idx == day_idx && c.start == prev->start_period &&
          c.room_idx == room_idx) {
          chosen = ci;
          break;
        }
      }
      if (chosen < 0) continue;  // placement no longer feasible (e.g. blocked)
      for (int ci : cs.by_lesson[li]) {
        cp.AddHint(x[ci], ci == chosen);
      }
    }
  }

}  // namespace arrango
