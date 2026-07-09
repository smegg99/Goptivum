// src/solve/lns.cc

#include "solve/lns.h"

#include <algorithm>
#include <numeric>
#include <random>
#include <set>
#include <string>
#include <unordered_map>
#include <vector>

#include "model/index.h"
#include "score/scorer.h"
#include "solve/trace.h"
#include "validate/validator.h"

namespace arrango {
  namespace {

    using PlaceMap = std::unordered_map<Id, Placement>;

    // Which lessons a neighborhood may re-optimize. A lesson moves only when
    // EVERY participant division is freed: a cross-division merged lesson in a
    // partial neighborhood would lose the missing division's student
    // no-overlap (its atoms don't exist in the reduced model). Parallel blocks
    // move as a unit for the same reason — one fixed member pins the block.
    std::vector<bool> FreedLessons(const SchoolModel& m, const ModelIndex& ix,
      const std::set<int>& freed) {
      std::vector<bool> movable(m.lessons.size(), false);
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        // Movable = at least one resolvable division AND every one of them
        // is freed. Lessons with only unknown divisions stay fixed.
        bool any = false, all = true;
        for (const Participant& p : m.lessons[li].participants) {
          int c = ix.DivisionIdx(p.division_id);
          if (c < 0) continue;
          any = true;
          all = all && freed.count(c) > 0;
        }
        movable[li] = any && all;
      }
      for (const auto& block : ix.ParallelBlocks()) {
        bool all = true;
        for (int li : block) all = all && movable[li];
        if (!all) {
          for (int li : block) movable[li] = false;
        }
      }
      // Lesson links move as a unit too — same reasoning as blocks.
      for (const LessonLink& link : m.lesson_links) {
        bool all = true;
        std::vector<int> members;
        for (Id id : link.lesson_ids) {
          int li = ix.LessonIdx(id);
          if (li < 0) continue;
          members.push_back(li);
          all = all && movable[li];
        }
        if (!all) {
          for (int li : members) movable[li] = false;
        }
      }
      return movable;
    }

    // Builds a small model that optimizes only the `movable` lessons.
    // Non-freed divisions are removed; every other lesson becomes fixed
    // occupancy (external blocks) at its incumbent slot — teacher, room, and
    // any participant belonging to a freed division, so freed lessons cannot
    // collide with fixed ones on any shared resource or student set. Freed
    // lessons carry their incumbent as a previous_placement so the sub-solve
    // warm-starts from — and never regresses below — the current schedule.
    SchoolModel BuildReduced(const SchoolModel& m, const ModelIndex& ix,
      const std::set<int>& freed, const std::vector<bool>& movable,
      const PlaceMap& place) {
      SchoolModel red;
      red.name = m.name;
      red.days = m.days;
      red.periods = m.periods;
      red.years = m.years;
      red.teachers = m.teachers;
      red.subjects = m.subjects;
      red.rooms = m.rooms;
      red.weights = m.weights;
      red.preferences = m.preferences;
      red.daily_load_rules = m.daily_load_rules;
      red.external_blocks = m.external_blocks;

      std::set<Id> freed_div_ids;
      for (int c : freed) {
        red.divisions.push_back(m.divisions[c]);
        freed_div_ids.insert(m.divisions[c].id);
      }
      for (const Group& g : m.groups) {
        if (freed_div_ids.count(g.division_id)) red.groups.push_back(g);
      }
      // Without the splits, the sub-model's groups would all fall back to
      // implicit open splits — wrong streams AND wrong hard semantics.
      for (const Split& s : m.splits) {
        if (freed_div_ids.count(s.division_id)) red.splits.push_back(s);
      }
      // Rule philosophy travels whole: overrides for non-freed entities
      // simply never match in the sub-model.
      red.rule_config = m.rule_config;
      // Links whose members are all movable travel too (the unit closure
      // above guarantees no link is ever half-freed).
      for (const LessonLink& link : m.lesson_links) {
        bool all = true;
        for (Id id : link.lesson_ids) {
          int li = ix.LessonIdx(id);
          all = all && li >= 0 && movable[li];
        }
        if (all) red.lesson_links.push_back(link);
      }

      // Block ids only need to be unique; nothing in the solver reads them.
      Id next_block_id = 1;
      for (const ExternalBlock& b : m.external_blocks) {
        next_block_id = std::max(next_block_id, b.id + 1);
      }

      for (size_t li = 0; li < m.lessons.size(); ++li) {
        const LessonInstance& l = m.lessons[li];
        if (movable[li]) {
          LessonInstance freed_lesson = l;
          auto it = place.find(l.id);
          if (it != place.end()) freed_lesson.previous_placement = it->second;
          red.lessons.push_back(std::move(freed_lesson));
          continue;
        }
        // Fixed lesson: reserve its teacher, room, and freed-division student
        // sets at the incumbent slot.
        auto it = place.find(l.id);
        if (it == place.end()) continue;
        const Placement& p = it->second;
        auto reserve = [&] (BlockTarget target, Id target_id) {
          red.external_blocks.push_back({ next_block_id++, "fixed", target,
                                         target_id, p.day_id, p.start_period,
                                         l.duration });
          };
        if (l.requires_teacher && l.teacher_id != kNoId) {
          reserve(BlockTarget::kTeacher, l.teacher_id);
        }
        if (p.room_id != kNoId) reserve(BlockTarget::kRoom, p.room_id);
        for (const Participant& part : l.participants) {
          int c = ix.DivisionIdx(part.division_id);
          if (c < 0 || freed.count(c) == 0) continue;  // not in the sub-model
          if (part.group_id != kNoId) {
            reserve(BlockTarget::kGroup, part.group_id);
          }
          else {
            reserve(BlockTarget::kDivision, part.division_id);
          }
        }
      }
      return red;
    }

    // Snapshot from a placement map (order is irrelevant to scoring).
    ScheduleSnapshot SnapshotOf(const PlaceMap& place) {
      ScheduleSnapshot s;
      s.lessons.reserve(place.size());
      for (const auto& [id, p] : place) s.lessons.push_back({ id, p });
      return s;
    }

    // Total config-hard violations: a construct incumbent can carry them
    // (hard-rule encodings live with the objective, which construct skips),
    // and they cost ZERO penalty by design — so acceptance is lexicographic
    // (hard, penalty), never penalty alone.
    int64_t HardViolations(const ScoreReport& report) {
      int64_t count = 0;
      for (const SoftIssue& issue : report.soft_issues) {
        if (issue.config_hard) count += issue.count;
      }
      return count;
    }

    // The polish loop's state in one place. Run() is the loop; every other
    // method does exactly one step of it.
    class Polisher {
    public:
      Polisher(const SchoolModel& m, const ScheduleSnapshot& incumbent,
        const SolveParams& budget, std::atomic<bool>* cancel,
        const ProgressFn& on_improved,
        const std::function<double()>& elapsed, const StageSink& on_stage)
        : m_(m),
          budget_(budget),
          cancel_(cancel),
          on_improved_(on_improved),
          elapsed_(elapsed),
          on_stage_(on_stage),
          // Score many trial snapshots of ONE unchanged model: derive the
          // scoring context (index/atoms/streams) once, not per trial.
          score_context_(ScoringContext::Build(m)),
          ix_(score_context_.index),
          div_count_(m.divisions.size()),
          division_penalty_(div_count_, 0),
          division_hard_(div_count_, 0),
          deadline_(budget.max_time_seconds),
          // Per-neighborhood time slice; small enough for many neighborhoods,
          // large enough to make progress on a few divisions. Config may
          // override.
          slice_(budget.lns_seconds_per_neighborhood > 0.0
            ? budget.lns_seconds_per_neighborhood
            : std::clamp(budget.max_time_seconds * 0.04, 3.0, 15.0)),
          // Divisions re-optimized together (config may override; default 3).
          chunk_(budget.lns_neighborhood_divisions > 0
            ? static_cast<int>(budget.lns_neighborhood_divisions)
            : 3),
          rng_(static_cast<uint64_t>(budget.random_seed) +
            0x9e3779b97f4a7c15ULL),
          order_(div_count_) {
        for (const ScheduledLesson& sl : incumbent.lessons) {
          place_[sl.lesson_id] = sl.placement;
        }
        const ScoreReport report = ComputeScore(score_context_, incumbent);
        best_penalty_ = report.total_penalty;
        best_hard_ = HardViolations(report);
        NoteDivisionScores(report);
        std::iota(order_.begin(), order_.end(), 0);
        // Live counters for the stage sink; one event per neighborhood.
        progress_.stage = SolveStage::kLns;
        progress_.lns_pass = 1;
        progress_.lns_neighborhoods_total = static_cast<uint32_t>(
          (div_count_ + static_cast<size_t>(chunk_) - 1) / chunk_);
        lns_started_ = elapsed_();
      }

      ScheduleSnapshot Run() {
        trace::Line("lns: polish start penalty=%ld hard_violations=%ld "
          "deadline=%.0fs slice=%.1fs chunk=%d divisions=%zu",
          best_penalty_, best_hard_, deadline_, slice_, chunk_, div_count_);
        StartPass(/*worst_first=*/true);
        while (elapsed_() < deadline_ - 0.5) {
          if (cancel_ != nullptr && cancel_->load()) break;
          if (deadline_ - elapsed_() < 1.0) break;
          TryNeighborhood(NextNeighborhood());
        }
        return SnapshotOf(place_);
      }

    private:
      // Per-division penalties steer the neighborhood order (worst first);
      // config-hard violation counts ride along and OUTRANK penalties — a
      // hard violation is an ERROR the polish must clear before comfort.
      void NoteDivisionScores(const ScoreReport& report) {
        for (size_t c = 0; c < div_count_; ++c) {
          division_penalty_[c] = report.division_scores[c].penalty;
          division_hard_[c] = 0;
        }
        for (const SoftIssue& issue : report.soft_issues) {
          if (!issue.config_hard || issue.teacher) continue;
          for (size_t c = 0; c < div_count_; ++c) {
            if (report.division_scores[c].entity_id == issue.entity_id) {
              division_hard_[c] += issue.count;
            }
          }
        }
      }

      // Order for one pass: while passes keep improving, visit the
      // worst-scored divisions first (fix where it hurts; the pre-shuffle
      // makes equal-penalty divisions rotate). After a pass with no
      // improvement, fall back to a pure shuffle, which pairs different
      // (often teacher-coupled) divisions together and escapes the
      // deterministic rut.
      void StartPass(bool worst_first) {
        std::shuffle(order_.begin(), order_.end(), rng_);
        if (worst_first) {
          // Hard violations outrank penalties: repair errors first.
          std::stable_sort(order_.begin(), order_.end(), [&] (int a, int b) {
            if (division_hard_[a] != division_hard_[b]) {
              return division_hard_[a] > division_hard_[b];
            }
            return division_penalty_[a] > division_penalty_[b];
            });
        }
      }

      // The next `chunk_` divisions of the pass; rolls into a fresh pass
      // when the current one is exhausted.
      std::set<int> NextNeighborhood() {
        if (cursor_ >= div_count_) {  // finished a pass
          StartPass(/*worst_first=*/pass_improved_);
          pass_improved_ = false;
          cursor_ = 0;
          ++progress_.lns_pass;
          progress_.lns_neighborhood = 0;
        }
        std::set<int> freed;
        for (int k = 0; k < chunk_ && cursor_ < div_count_; ++k) {
          freed.insert(order_[cursor_++]);
        }
        ++progress_.lns_neighborhood;
        return freed;
      }

      std::string NamesOf(const std::set<int>& freed) const {
        std::string names;
        for (int c : freed) {
          if (!names.empty()) names += ",";
          names += m_.divisions[c].name;
        }
        return names;
      }

      // One live event per finished neighborhood.
      void Emit(std::string detail) {
        if (!on_stage_) return;
        progress_.detail = std::move(detail);
        progress_.stage_elapsed_seconds = elapsed_() - lns_started_;
        on_stage_(progress_);
      }

      // Sub-solves the freed divisions against the fixed rest, then accepts
      // the trial iff it is lexicographically better (hard, penalty) AND
      // passes the full hard validator.
      void TryNeighborhood(const std::set<int>& freed) {
        const std::string names = NamesOf(freed);
        const std::vector<bool> movable = FreedLessons(m_, ix_, freed);
        SchoolModel reduced = BuildReduced(m_, ix_, freed, movable, place_);
        SolveParams sp;
        sp.max_time_seconds = std::min(slice_, deadline_ - elapsed_());
        sp.skip_preflight = true;  // sub-model derives from a valid incumbent
        sp.rule_config = budget_.rule_config;
        sp.num_workers = budget_.num_workers;
        // A fresh seed per neighborhood diversifies revisits of the same
        // divisions instead of replaying the identical search.
        sp.random_seed = budget_.random_seed + ++iteration_;
        sp.unknown_capacity_policy = budget_.unknown_capacity_policy;

        SolveResult sub;
        {
          trace::ScopedQuiet quiet;  // don't log the per-neighborhood sub-solve
          sub = SolveSchedule(reduced, sp, cancel_, nullptr, nullptr);
        }
        if (sub.best.lessons.empty()) {
          ++progress_.lns_rejected_worse;
          trace::Line("lns: freed {%s}  sub-solve EMPTY status=%d msg=%s "
            "(t=%.1fs)", names.c_str(), static_cast<int>(sub.status),
            sub.message.c_str(), elapsed_());
          Emit("{" + names + "} found nothing");
          return;
        }

        // Trial = current schedule with the freed lessons moved to their new
        // spots; fixed lessons keep their placements from `place_`.
        PlaceMap trial = place_;
        for (const ScheduledLesson& sl : sub.best.lessons) {
          trial[sl.lesson_id] = sl.placement;
        }
        const ScheduleSnapshot trial_snap = SnapshotOf(trial);
        const ScoreReport trial_report = ComputeScore(score_context_, trial_snap);
        const int64_t pen = trial_report.total_penalty;
        const int64_t hard = HardViolations(trial_report);
        const bool better =
          hard < best_hard_ || (hard == best_hard_ && pen < best_penalty_);
        if (!better) {
          ++progress_.lns_rejected_worse;
          trace::Line("lns: freed {%s}  no improvement: trial %ld >= best %ld "
            "(sub obj=%ld status=%d, t=%.1fs)", names.c_str(), pen,
            best_penalty_, sub.objective, static_cast<int>(sub.status),
            elapsed_());
          Emit("{" + names + "} no improvement");
          return;
        }
        // Hard-validity gate: the sub-model is built to make hard violations
        // impossible, so a failure here is a solver bug — never ship it.
        const ValidationReport gate = Validate(m_, trial_snap);
        if (!gate.valid) {
          trace::Line("lns: REJECTED hard-invalid trial (sub-model bug?) "
            "penalty %ld -> %ld  first conflict: kind=%d %s", best_penalty_,
            pen,
            gate.conflicts.empty()
              ? -1 : static_cast<int>(gate.conflicts.front().kind),
            gate.conflicts.empty()
              ? "" : gate.conflicts.front().message.c_str());
          ++progress_.lns_rejected_invalid;
          Emit("{" + names + "} REJECTED: hard-invalid trial");
          return;
        }
        Accept(names, std::move(trial), trial_snap, trial_report);
      }

      // Adopts a validated better trial as the new incumbent and streams it.
      void Accept(const std::string& names, PlaceMap trial,
        const ScheduleSnapshot& trial_snap, const ScoreReport& trial_report) {
        const int64_t pen = trial_report.total_penalty;
        const int64_t hard = HardViolations(trial_report);
        trace::Line("lns: freed {%s}  penalty %ld -> %ld  hard %ld -> %ld "
          "(t=%.1fs)",
          names.c_str(), best_penalty_, pen, best_hard_, hard, elapsed_());
        ++progress_.lns_accepted;
        Emit("{" + names + "} accepted " + std::to_string(best_penalty_) +
          " -> " + std::to_string(pen));
        best_penalty_ = pen;
        best_hard_ = hard;
        NoteDivisionScores(trial_report);
        pass_improved_ = true;
        place_ = std::move(trial);
        if (on_improved_) {
          SolveResult improved;
          improved.status = SolveStatusCode::kFeasible;
          improved.best = trial_snap;
          improved.objective = pen;
          improved.wall_time_seconds = elapsed_();
          on_improved_(improved);
        }
      }

      const SchoolModel& m_;
      const SolveParams& budget_;
      std::atomic<bool>* cancel_;
      const ProgressFn& on_improved_;
      const std::function<double()>& elapsed_;
      const StageSink& on_stage_;
      const ScoringContext score_context_;
      const ModelIndex& ix_;
      const size_t div_count_;

      PlaceMap place_;
      std::vector<int64_t> division_penalty_;
      std::vector<int64_t> division_hard_;
      int64_t best_penalty_{ 0 };
      int64_t best_hard_{ 0 };

      const double deadline_;
      const double slice_;
      const int chunk_;
      std::mt19937_64 rng_;
      std::vector<int> order_;
      size_t cursor_{ 0 };
      bool pass_improved_{ false };
      int64_t iteration_{ 0 };
      SolveProgressInfo progress_;
      double lns_started_{ 0.0 };
    };

  }  // namespace

  ScheduleSnapshot PolishByLns(const SchoolModel& m,
    const ScheduleSnapshot& incumbent,
    const SolveParams& budget,
    std::atomic<bool>* cancel,
    const ProgressFn& on_improved,
    const std::function<double()>& elapsed,
    const StageSink& on_stage) {
    if (m.divisions.empty()) return incumbent;
    return Polisher(m, incumbent, budget, cancel, on_improved, elapsed,
      on_stage)
      .Run();
  }

}  // namespace arrango
