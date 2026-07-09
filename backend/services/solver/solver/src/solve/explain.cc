// src/solve/explain.cc

#include "solve/explain.h"

#include <algorithm>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <map>
#include <set>
#include <utility>

#include "model/structure.h"
#include "ortools/sat/cp_model.h"
#include "score/penalty_defs.h"
#include "solve/candidates.h"
#include "solve/constraints.h"
#include "solve/objective.h"

namespace arrango {
  namespace {

    namespace sat = operations_research::sat;
    using Clock = std::chrono::steady_clock;

    double SecondsLeft(Clock::time_point deadline) {
      return std::chrono::duration<double>(deadline - Clock::now()).count();
    }

    bool DebugLog() {
      return std::getenv("ARRANGO_EXPLAIN_DEBUG") != nullptr;
    }

    // Single-worker bounded probe; the explain model is rebuilt once and
    // re-probed by swapping assumptions, so probes stay cheap.
    sat::CpSolverResponse Probe(sat::CpModelBuilder& cp, double seconds) {
      sat::Model model;
      sat::SatParameters params;
      params.set_max_time_in_seconds(std::max(seconds, 0.1));
      params.set_num_search_workers(1);
      model.Add(sat::NewSatParameters(params));
      return sat::SolveCpModel(cp.Build(), &model);
    }

    // One relaxable input with the assumption literal that keeps it enforced.
    struct Suspect {
      CoreItem item;
      sat::BoolVar lit;
    };

    // Does this candidate placement overlap the block's time window?
    bool OverlapsBlock(const ExternalBlock& blk, const ModelIndex& ix,
      int day_idx, uint32_t start, uint32_t duration) {
      if (ix.DayIdx(blk.day_id) != day_idx) return false;
      return start < blk.start_period + blk.duration &&
        blk.start_period < start + duration;
    }

    // Two switches per division: "all of its lessons are placed"
    // (completeness) and "its daily min/max bounds hold". The BUILT-IN daily
    // minimum applies even with zero configured daily-load rules, so the
    // bounds switch must always exist.
    void AddDivisionGates(const SchoolModel& m, sat::CpModelBuilder& cp,
      ExplainGates& gates, std::vector<Suspect>& suspects) {
      for (const Division& division : m.divisions) {
        sat::BoolVar lit = cp.NewBoolVar();
        gates.completeness_of_division[division.id] = lit;
        suspects.push_back({ { "placement_completeness", "", division.id,
                              division.name, {},
                              "placing all of " + division.name +
                              "'s lessons" },
                            lit });
        sat::BoolVar bounds = cp.NewBoolVar();
        gates.bounds_of_division[division.id] = bounds;
        suspects.push_back({ { "daily_bounds", "", division.id, division.name,
                              {}, division.name + "'s daily min/max bounds" },
                            bounds });
      }
    }

    // Availability switches: one per block TARGET; the switch enforces
    // "candidates this block would have pruned stay unpicked". Room blocks
    // are skipped — rooms stay factual.
    void AddAvailabilitySwitches(const SchoolModel& m, const ModelIndex& ix,
      const CandidateSet& cs, const std::vector<sat::BoolVar>& x,
      sat::CpModelBuilder& cp, std::vector<Suspect>& suspects) {
      std::map<std::pair<int, Id>, size_t> switch_of_target;  // -> suspects idx
      auto target_switch = [&] (const ExternalBlock& blk) -> sat::BoolVar {
        const char* kind = blk.target == BlockTarget::kTeacher
          ? "teacher_availability"
          : blk.target == BlockTarget::kDivision ? "division_availability"
          : "group_availability";
        std::string name;
        if (blk.target == BlockTarget::kTeacher) {
          int t = ix.TeacherIdx(blk.target_id);
          name = t >= 0 ? m.teachers[t].name : "?";
        }
        else if (blk.target == BlockTarget::kDivision) {
          int c = ix.DivisionIdx(blk.target_id);
          name = c >= 0 ? m.divisions[c].name : "?";
        }
        else {
          int g = ix.GroupIdx(blk.target_id);
          name = g >= 0 ? m.groups[g].name : "?";
        }
        auto key = std::make_pair(static_cast<int>(blk.target), blk.target_id);
        auto it = switch_of_target.find(key);
        if (it != switch_of_target.end()) return suspects[it->second].lit;
        sat::BoolVar lit = cp.NewBoolVar();
        switch_of_target[key] = suspects.size();
        suspects.push_back({ { kind, "", blk.target_id, name, {},
                              name + "'s availability" },
                            lit });
        return lit;
        };
      for (const ExternalBlock& blk : m.external_blocks) {
        if (blk.target == BlockTarget::kRoom) continue;  // rooms stay factual
        sat::BoolVar lit = target_switch(blk);
        for (size_t ci = 0; ci < cs.all.size(); ++ci) {
          const Candidate& c = cs.all[ci];
          const LessonInstance& l = m.lessons[c.lesson_idx];
          if (!OverlapsBlock(blk, ix, c.day_idx, c.start, l.duration)) continue;
          bool hits = false;
          if (blk.target == BlockTarget::kTeacher) {
            hits = l.requires_teacher && l.teacher_id == blk.target_id;
          }
          else if (blk.target == BlockTarget::kDivision) {
            for (const Participant& part : l.participants) {
              hits |= part.division_id == blk.target_id;
            }
          }
          else {  // group: same shared-students rule candidates.cc uses
            int g = ix.GroupIdx(blk.target_id);
            if (g >= 0) {
              const Participant blocked{ m.groups[g].division_id,
                                        m.groups[g].id };
              for (const Participant& part : l.participants) {
                hits |= SharesStudents(m, ix, part, blocked);
              }
            }
          }
          if (hits) {
            cp.AddEquality(sat::LinearExpr(x[ci]), 0).OnlyEnforceIf(lit);
          }
        }
      }
    }

    // Lock switches: the switch pins the lesson to its locked placement.
    void AddLockSwitches(const SchoolModel& m, const ModelIndex& ix,
      const CandidateSet& cs, const std::vector<sat::BoolVar>& x,
      sat::CpModelBuilder& cp, std::vector<Suspect>& suspects) {
      for (size_t li = 0; li < m.lessons.size(); ++li) {
        const LessonInstance& l = m.lessons[li];
        if (!l.locked || !l.locked_placement) continue;
        sat::BoolVar lit = cp.NewBoolVar();
        suspects.push_back({ { "locked_lesson", "", kNoId, "", { l.id },
                              "locked lesson " + std::to_string(l.id) },
                            lit });
        const Placement& p = *l.locked_placement;
        for (int ci : cs.by_lesson[li]) {
          const Candidate& c = cs.all[ci];
          const bool matches = ix.DayIdx(p.day_id) == c.day_idx &&
            p.start_period == c.start &&
            (p.room_id == kNoId ? c.room_idx < 0
              : (c.room_idx >= 0 && m.rooms[c.room_idx].id == p.room_id));
          if (!matches) {
            cp.AddEquality(sat::LinearExpr(x[ci]), 0).OnlyEnforceIf(lit);
          }
        }
      }
    }

    // The suspects CP-SAT proved sufficient for infeasibility. Empty when
    // presolve proved infeasibility without attributing assumptions (a
    // CP-SAT quirk) — the caller then shrinks over ALL suspects instead.
    std::vector<size_t> AttributedCore(const sat::CpSolverResponse& response,
      const std::vector<Suspect>& suspects) {
      std::set<int> core_indices(
        response.sufficient_assumptions_for_infeasibility().begin(),
        response.sufficient_assumptions_for_infeasibility().end());
      std::vector<size_t> in_core;
      for (size_t i = 0; i < suspects.size(); ++i) {
        if (core_indices.count(suspects[i].lit.index())) in_core.push_back(i);
      }
      return in_core;
    }

    // Deletion shrink: drop one member; still infeasible => provably not
    // needed. A probe timing out keeps the member conservatively and clears
    // `minimal`.
    void ShrinkCore(sat::CpModelBuilder& cp,
      const std::vector<Suspect>& suspects, Clock::time_point deadline,
      double budget_seconds, std::vector<size_t>& in_core,
      InfeasibleCore& core) {
      for (size_t probe = 0; probe < in_core.size();) {
        if (SecondsLeft(deadline) < 0.5) {
          core.minimal = false;
          break;
        }
        std::vector<sat::BoolVar> subset;
        for (size_t j = 0; j < in_core.size(); ++j) {
          if (j != probe) subset.push_back(suspects[in_core[j]].lit);
        }
        cp.ClearAssumptions();
        cp.AddAssumptions(subset);
        sat::CpSolverResponse r = Probe(
          cp, std::min(SecondsLeft(deadline),
                       budget_seconds / (in_core.size() + 2)));
        if (DebugLog()) {
          std::cerr << "explain: shrink probe drop=" << probe
                    << " subset=" << subset.size()
                    << " status=" << r.status() << "\n";
        }
        if (r.status() == sat::CpSolverStatus::INFEASIBLE) {
          in_core.erase(in_core.begin() + probe);  // provably not needed
        }
        else {
          if (r.status() != sat::CpSolverStatus::OPTIMAL &&
            r.status() != sat::CpSolverStatus::FEASIBLE) {
            core.minimal = false;  // probe timed out: keep, conservatively
          }
          ++probe;
        }
      }
    }

    // Rule deletion loop (school-wide granularity): relax one active hard
    // rule to soft; if the ORIGINAL model becomes solvable, the rule is part
    // of the story.
    void AddHardRuleSuspects(const SchoolModel& m, const ModelIndex& ix,
      const RuleConfig& request_rules, const SolveParams& params,
      Clock::time_point deadline, double budget_seconds,
      InfeasibleCore& core) {
      for (const RuleDef& def : RuleTable()) {
        if (SecondsLeft(deadline) < 0.5) {
          core.minimal = false;
          break;
        }
        const RuleResolver original =
          RuleResolver::Build(m, ix, request_rules);
        if (original.SchoolWide(def.key).mode != RuleMode::kHard) continue;
        SolveParams probe_params = params;
        probe_params.max_time_seconds = std::min(
          SecondsLeft(deadline), std::max(budget_seconds * 0.2, 1.0));
        probe_params.disable_lns = true;
        probe_params.skip_preflight = true;
        probe_params.num_workers = 1;
        probe_params.rule_config = request_rules;
        probe_params.rule_config.overrides.push_back(
          { def.key, RuleMode::kSoft });
        SolveResult relaxed_solve =
          SolveSchedule(m, probe_params, nullptr, nullptr);
        if (relaxed_solve.status == SolveStatusCode::kOptimal ||
          relaxed_solve.status == SolveStatusCode::kFeasible) {
          core.items.push_back({ "hard_rule", def.key, kNoId, "", {},
                                std::string("the hard '") + def.key +
                                "' rule" });
        }
        else if (relaxed_solve.status != SolveStatusCode::kInfeasible) {
          core.minimal = false;  // timeout: unattributed, stay honest
        }
      }
    }

  }  // namespace

  std::optional<InfeasibleCore> ExplainInfeasibility(const SchoolModel& m,
    const ModelIndex& ix, const RuleConfig& request_rules,
    const SolveParams& params, double budget_seconds) {
    const Clock::time_point deadline =
      Clock::now() + std::chrono::duration_cast<Clock::duration>(
        std::chrono::duration<double>(budget_seconds));
    InfeasibleCore core;

    // The explain copy: blocks and locks removed from the MODEL and re-added
    // as switch-gated constraints, so the core can name them.
    SchoolModel relaxed = m;
    relaxed.external_blocks.clear();
    for (LessonInstance& lesson : relaxed.lessons) lesson.locked = false;
    ModelIndex rix = ModelIndex::Build(relaxed);
    const RuleResolver rules = RuleResolver::Build(relaxed, rix, request_rules);

    auto built = BuildCandidates(relaxed, rix, params.unknown_capacity_policy,
      params.max_candidates_per_lesson, &rules);
    if (!std::holds_alternative<CandidateSet>(built)) {
      // Even unlocked and unblocked, some lesson has nowhere to go — that
      // message IS the explanation.
      core.items.push_back({ "placement_completeness", "", kNoId, "",
                            {}, std::get<std::string>(built) });
      core.message = std::get<std::string>(built);
      return core;
    }
    const CandidateSet& cs = std::get<CandidateSet>(built);

    sat::CpModelBuilder cp;
    std::vector<sat::BoolVar> x;
    x.reserve(cs.all.size());
    for (size_t i = 0; i < cs.all.size(); ++i) x.push_back(cp.NewBoolVar());

    std::vector<Suspect> suspects;
    ExplainGates gates;
    AddDivisionGates(m, cp, gates, suspects);
    AddHardConstraints(relaxed, rix, cs, x, cp, &gates);
    // Hard rules stay ENFORCED in this pass (encoded by the objective
    // builder); the rule deletion loop attributes them by name.
    AddSoftObjective(relaxed, rix, cs, x, cp, rules);
    AddAvailabilitySwitches(m, ix, cs, x, cp, suspects);
    AddLockSwitches(m, ix, cs, x, cp, suspects);

    // Assumption pass: everything enforced == the original model.
    std::vector<sat::BoolVar> all_lits;
    for (const Suspect& s : suspects) all_lits.push_back(s.lit);
    cp.AddAssumptions(all_lits);
    sat::CpSolverResponse first =
      Probe(cp, std::min(budget_seconds * 0.35, SecondsLeft(deadline)));
    if (DebugLog()) {
      std::cerr << "explain: first probe status=" << first.status()
                << " suspects=" << suspects.size() << " core_size="
                << first.sufficient_assumptions_for_infeasibility().size()
                << "\n";
    }
    if (first.status() == sat::CpSolverStatus::INFEASIBLE) {
      std::vector<size_t> in_core = AttributedCore(first, suspects);
      if (in_core.empty()) {
        // Presolve proved infeasibility without attributing assumptions:
        // fall back to deletion over ALL suspects — the shrink loop
        // minimizes it under the budget.
        for (size_t i = 0; i < suspects.size(); ++i) in_core.push_back(i);
      }
      ShrinkCore(cp, suspects, deadline, budget_seconds, in_core, core);
      for (size_t i : in_core) core.items.push_back(suspects[i].item);
    }
    else if (first.status() == sat::CpSolverStatus::OPTIMAL ||
      first.status() == sat::CpSolverStatus::FEASIBLE) {
      // Everything gated is enforced and it SOLVES: the contradiction needs
      // a hard rule interaction the gates cannot express (e.g. a rule vs
      // itself across divisions). The rule loop below finds it.
    }
    else {
      core.minimal = false;  // first probe died on the budget
    }

    AddHardRuleSuspects(m, ix, request_rules, params, deadline,
      budget_seconds, core);

    if (core.items.empty()) return std::nullopt;  // budget beat us entirely
    core.message = "cannot satisfy together: ";
    for (size_t i = 0; i < core.items.size(); ++i) {
      if (i > 0) core.message += ", ";
      core.message += core.items[i].message;
    }
    return core;
  }

  std::vector<CoreItem> TimeoutHints(const SchoolModel& m,
    const ModelIndex& ix) {
    std::vector<CoreItem> hints;
    int64_t week = 0;
    for (const Day& d : m.days) week += d.period_count;
    if (week == 0) return hints;
    // Tight room pools: demand close to pool capacity.
    std::map<std::vector<std::string>, int64_t> demand;
    for (const LessonInstance& l : m.lessons) {
      if (!l.requires_room || l.allowed_room_designators.empty()) continue;
      std::vector<std::string> key = l.allowed_room_designators;
      std::sort(key.begin(), key.end());
      demand[std::move(key)] += l.duration;
    }
    for (const auto& [pool, periods] : demand) {
      int64_t rooms = 0;
      for (const Room& r : m.rooms) {
        rooms += std::count(pool.begin(), pool.end(), r.designator) > 0;
      }
      if (rooms > 0 && periods * 10 >= rooms * week * 8) {  // >= 80% full
        std::string names;
        for (const std::string& d : pool) {
          if (!names.empty()) names += ",";
          names += d;
        }
        hints.push_back({ "hint", "", kNoId, names, {},
                         "room pool {" + names + "} is " +
                         std::to_string(periods * 100 / (rooms * week)) +
                         "% booked" });
      }
    }
    // Heaviest teachers.
    for (size_t t = 0; t < m.teachers.size(); ++t) {
      int64_t load = 0;
      for (int li : ix.LessonsOfTeacher(static_cast<int>(t))) {
        load += m.lessons[li].duration;
      }
      if (load * 10 >= week * 8) {
        hints.push_back({ "hint", "", m.teachers[t].id, m.teachers[t].name,
                         {}, "teacher " + m.teachers[t].name + " is " +
                         std::to_string(load * 100 / week) + "% booked" });
      }
    }
    // Stream-heaviest divisions (fixed-split products).
    const std::vector<int64_t> streams = StreamCountPerDivision(m, ix);
    for (size_t c = 0; c < streams.size(); ++c) {
      if (streams[c] >= 8) {
        hints.push_back({ "hint", "", m.divisions[c].id, m.divisions[c].name,
                         {}, "division " + m.divisions[c].name + " fans into " +
                         std::to_string(streams[c]) + " student streams" });
      }
    }
    return hints;
  }

}  // namespace arrango
