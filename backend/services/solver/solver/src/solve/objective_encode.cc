// src/solve/objective_encode.cc

#include <vector>

#include "solve/objective_internal.h"

// Generic CP-SAT encoding of "occupancy over a day" and the two penalties
// derived from it — gaps and split runs. These are the intricate bits: each
// function turns a row of Cells (see objective_internal.h) into boolean
// variables and constraints whose value, on every feasible solution, equals the
// exact penalty the scorer computes by plain counting. Keeping the two in lock
// step is what the ObjectiveMatchesScorerPenalty drift test guards.
namespace arrango {
namespace objective_detail {

// One occupancy cell per period: True if externally blocked, False if no
// candidate can cover it, else a fresh var `occ` channeled to occ == OR(covers)
// (occ >= each cover, and occ <= their sum).
std::vector<Cell> Builder::MakeCells(
    uint32_t period_count,
    const std::vector<std::vector<sat::BoolVar>>& covers,
    const std::vector<bool>& blocked) {
  std::vector<Cell> cells(period_count);
  for (uint32_t q = 0; q < period_count; ++q) {
    const bool forced = blocked[q];
    const std::vector<sat::BoolVar>& vars = covers[q];
    if (forced) {
      cells[q] = Cell::True();
    }
    else if (vars.empty()) {
      cells[q] = Cell::False();
    }
    else {
      sat::BoolVar occ = cp_.NewBoolVar();
      sat::LinearExpr sum;
      for (const sat::BoolVar& v : vars) {
        cp_.AddImplication(v, occ);  // occ >= v
        sum += v;
      }
      cp_.AddLessOrEqual(occ, sum);  // occ <= sum(v)
      cells[q] = Cell::Var(occ);
    }
  }
  return cells;
}

// Gap penalty for one entity-day: an empty period counts as a gap when it sits
// strictly between an occupied period before it and an occupied period after
// it. We can't just look left/right one step (gaps can be several periods
// wide), so we build prefix/suffix "is anything occupied up to / from here"
// booleans and define gap[p] = before[p-1] AND after[p+1] AND NOT occ[p].
//   `capped` limits the counted gaps to gap_cap_per_day (teacher gaps).
//   gap_rule / window_rule pick each part's exit: soft = weighted cost,
//   hard = the violation variables forced to zero, off = ignored. The
//   window rule (adjacent gap pairs) defaults to HARD — a >= 2-period hole
//   is disallowed outright unless the school softens it.
void Builder::AddGapTerm(const std::vector<Cell>& cells, int64_t weight,
                         bool capped, const ResolvedRule& gap_rule,
                         const ResolvedRule& window_rule,
                         int64_t window_weight) {
  const size_t n = cells.size();
  if (n < 3) return;
  const bool want_gaps = gap_rule.mode != RuleMode::kOff &&
                         (gap_rule.mode == RuleMode::kHard || weight != 0);
  const bool want_windows =
      window_rule.mode != RuleMode::kOff &&
      (window_rule.mode == RuleMode::kHard || window_weight != 0);
  if (!want_gaps && !want_windows) return;

  // before[p] = OR(cells[0..p]); after[p] = OR(cells[p..n-1]). Exact channeling
  // (both bounds) so the aux values are functions of x and the objective equals
  // the scorer's penalty on every feasible solution.
  auto chain = [&] (bool forward) {
    std::vector<Cell> out(n);
    Cell acc = Cell::False();
    for (size_t i = 0; i < n; ++i) {
      size_t p = forward ? i : n - 1 - i;
      const Cell& c = cells[p];
      if (c.state == Cell::kTrue || acc.state == Cell::kTrue) {
        acc = Cell::True();
      }
      else if (c.state == Cell::kFalse) {
        // acc unchanged
      }
      else if (acc.state == Cell::kFalse) {
        acc = c;
      }
      else {
        sat::BoolVar b = cp_.NewBoolVar();
        cp_.AddImplication(acc.var, b);
        cp_.AddImplication(c.var, b);
        cp_.AddLessOrEqual(b, sat::LinearExpr(acc.var) + c.var);
        acc = Cell::Var(b);
      }
      out[p] = acc;
    }
    return out;
    };
  std::vector<Cell> before = chain(true);
  std::vector<Cell> after = chain(false);

  auto expr_of = [] (const Cell& c) {
    return c.state == Cell::kTrue ? sat::LinearExpr(1) : sat::LinearExpr(c.var);
    };

  sat::LinearExpr gap_sum;
  bool any = false;
  std::vector<Cell> gap_cells(n, Cell::False());
  for (size_t p = 1; p + 1 < n; ++p) {
    const Cell& occ = cells[p];
    if (occ.state == Cell::kTrue) continue;
    const Cell& b = before[p - 1];
    const Cell& a = after[p + 1];
    if (b.state == Cell::kFalse || a.state == Cell::kFalse) continue;
    if (b.state == Cell::kTrue && a.state == Cell::kTrue &&
      occ.state == Cell::kFalse) {
      gap_sum += 1;  // structurally forced gap
      gap_cells[p] = Cell::True();
      any = true;
      continue;
    }
    // gap == b AND a AND NOT occ, fully channeled.
    sat::BoolVar gap = cp_.NewBoolVar();
    sat::LinearExpr lower = expr_of(b) + expr_of(a) - 1;
    if (occ.state == Cell::kVar) lower -= occ.var;
    cp_.AddGreaterOrEqual(gap, lower);
    if (b.state == Cell::kVar) cp_.AddImplication(gap, b.var);
    if (a.state == Cell::kVar) cp_.AddImplication(gap, a.var);
    if (occ.state == Cell::kVar) {
      cp_.AddLessOrEqual(sat::LinearExpr(gap) + occ.var, 1);
    }
    gap_sum += gap;
    gap_cells[p] = Cell::Var(gap);
    any = true;
  }
  if (!any) return;

  // Window exit: adjacent gap pairs. HARD (the default) forbids them; SOFT
  // prices each pair exactly like the scorer's window_pairs count; OFF
  // ignores them.
  if (want_windows) {
    sat::LinearExpr window_sum;
    for (size_t p = 1; p + 2 < n; ++p) {
      const Cell& g1 = gap_cells[p];
      const Cell& g2 = gap_cells[p + 1];
      if (g1.state == Cell::kFalse || g2.state == Cell::kFalse) continue;
      if (window_rule.mode == RuleMode::kHard) {
        if (g1.state == Cell::kTrue && g2.state == Cell::kTrue) {
          // Structurally forced window (e.g. locked lessons): infeasible.
          cp_.AddLessOrEqual(sat::LinearExpr(2), 1);
        }
        else if (g1.state == Cell::kTrue) {
          cp_.AddEquality(sat::LinearExpr(g2.var), 0);
        }
        else if (g2.state == Cell::kTrue) {
          cp_.AddEquality(sat::LinearExpr(g1.var), 0);
        }
        else {
          cp_.AddLessOrEqual(sat::LinearExpr(g1.var) + g2.var, 1);
        }
      }
      else {  // soft: pair == g1 AND g2, exactly channeled
        if (g1.state == Cell::kTrue && g2.state == Cell::kTrue) {
          window_sum += 1;
        }
        else if (g1.state == Cell::kTrue) {
          window_sum += g2.var;
        }
        else if (g2.state == Cell::kTrue) {
          window_sum += g1.var;
        }
        else {
          sat::BoolVar pair = cp_.NewBoolVar();
          cp_.AddGreaterOrEqual(pair,
                                sat::LinearExpr(g1.var) + g2.var - 1);
          cp_.AddImplication(pair, g1.var);
          cp_.AddImplication(pair, g2.var);
          window_sum += pair;
        }
      }
    }
    if (window_rule.mode == RuleMode::kSoft) {
      objective_ += window_weight * window_sum;
    }
  }

  // Gap exit.
  if (!want_gaps) return;
  if (gap_rule.mode == RuleMode::kHard) {
    // No slot may be a gap at all — every gap variable is forced to zero,
    // and a structurally forced gap makes the model infeasible.
    for (size_t p = 1; p + 1 < n; ++p) {
      const Cell& g = gap_cells[p];
      if (g.state == Cell::kTrue) cp_.AddLessOrEqual(sat::LinearExpr(2), 1);
      else if (g.state == Cell::kVar) {
        cp_.AddEquality(sat::LinearExpr(g.var), 0);
      }
    }
    return;
  }
  if (!capped) {
    objective_ += weight * gap_sum;
    return;
  }
  sat::IntVar capped_var =
      cp_.NewIntVar({ 0, static_cast<int64_t>(w_.gap_cap_per_day) });
  cp_.AddMinEquality(
      capped_var,
      std::vector<sat::LinearExpr>{
      gap_sum, sat::LinearExpr(static_cast<int64_t>(w_.gap_cap_per_day))});
  objective_ += weight * capped_var;
}

// Split penalty: (number of separate runs - 1) per day for one subject. A "run"
// is a maximal block of consecutive occupied periods; two runs on a day means
// the subject was split, costing one penalty. run_start[p] = occ[p] AND NOT
// occ[p-1] counts the runs; subtracting one "active" (any occupancy at all)
// yields runs - 1 via a non-negative var so the objective stays positive.
// run_rule exits: soft prices runs - 1; hard allows at most ONE run per day
// (subject_once as a constraint — structurally forced doubles go
// infeasible); off skips the term entirely.
void Builder::AddRunTerm(const std::vector<Cell>& cells, int64_t weight,
                         const ResolvedRule& run_rule) {
  const size_t n = cells.size();
  if (n == 0 || run_rule.mode == RuleMode::kOff) return;
  if (run_rule.mode != RuleMode::kHard && weight == 0) return;
  sat::LinearExpr run_sum;
  bool all_false = true;
  bool any_true = false;
  for (size_t q = 0; q < n; ++q) {
    const Cell& cur = cells[q];
    if (cur.state != Cell::kFalse) all_false = false;
    if (cur.state == Cell::kTrue) any_true = true;
    const Cell prev = q == 0 ? Cell::False() : cells[q - 1];
    if (cur.state == Cell::kFalse || prev.state == Cell::kTrue) continue;
    if (cur.state == Cell::kTrue && prev.state == Cell::kFalse) {
      run_sum += 1;
      continue;
    }
    // rs == cur AND NOT prev, fully channeled.
    sat::BoolVar rs = cp_.NewBoolVar();
    sat::LinearExpr lower;
    lower += cur.state == Cell::kTrue ? sat::LinearExpr(1)
                                      : sat::LinearExpr(cur.var);
    if (prev.state == Cell::kVar) lower -= prev.var;
    cp_.AddGreaterOrEqual(rs, lower);
    if (cur.state == Cell::kVar) cp_.AddImplication(rs, cur.var);
    if (prev.state == Cell::kVar) {
      cp_.AddLessOrEqual(sat::LinearExpr(rs) + prev.var, 1);
    }
    run_sum += rs;
  }
  if (all_false) return;

  if (run_rule.mode == RuleMode::kHard) {
    // At most one run per day; run_sum already folds structurally forced
    // starts in as constants, so locked doubles turn infeasible here.
    cp_.AddLessOrEqual(run_sum, 1);
    return;
  }

  sat::LinearExpr active;
  if (any_true) {
    active += 1;
  }
  else {
    sat::BoolVar a = cp_.NewBoolVar();
    sat::LinearExpr occ_sum;
    for (const Cell& c : cells) {
      if (c.state == Cell::kVar) {
        occ_sum += c.var;
        cp_.AddImplication(c.var, a);  // a == OR(occ), exact
      }
    }
    cp_.AddLessOrEqual(a, occ_sum);
    active += a;
  }
  // Channel through a non-negative var: keeps every objective coefficient
  // positive so the lower bound closes instantly.
  sat::IntVar splits = cp_.NewIntVar({ 0, static_cast<int64_t>(n) });
  cp_.AddEquality(splits, run_sum - active);
  objective_ += weight * splits;
}

}  // namespace objective_detail
}  // namespace arrango
