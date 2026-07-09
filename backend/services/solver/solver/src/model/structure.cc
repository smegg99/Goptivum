// src/model/structure.cc

#include "model/structure.h"

namespace arrango {

  bool SharesStudents(const SchoolModel& m, const ModelIndex& ix,
    const Participant& a, const Participant& b) {
    (void)m;  // reserved: future rules may need model data beyond the index
    const int division_a = ix.DivisionIdx(a.division_id);
    const int division_b = ix.DivisionIdx(b.division_id);
    // A student belongs to exactly one division, so different (or unknown)
    // divisions can never share one.
    if (division_a < 0 || division_b < 0 || division_a != division_b) {
      return false;
    }
    // kNoId group means the whole division takes part: every student of the
    // division is in it, so it overlaps any other participant here.
    if (a.group_id == kNoId || b.group_id == kNoId) return true;
    const int group_a = ix.GroupIdx(a.group_id);
    const int group_b = ix.GroupIdx(b.group_id);
    if (group_a < 0 || group_b < 0) return false;  // validator's business
    if (group_a == group_b) return true;           // literally the same kids
    // -1 = the group has no split: it acts as its own private OPEN split.
    const int split_a = ix.EffectiveSplitOf(group_a);
    const int split_b = ix.EffectiveSplitOf(group_b);
    // Rule 1: two different groups of one split are disjoint by definition.
    if (split_a == split_b && split_a >= 0) return false;
    // Rule 2: open splits are planning constructs — the school assigns
    // students around the plan, so open-vs-open never constrains.
    if (!ix.SplitIsFixed(split_a) && !ix.SplitIsFixed(split_b)) return false;
    // Rule 3: fixed membership (gender, religion choice, trade) overlaps
    // everything outside its own split.
    return true;
  }

  std::vector<int64_t> StreamCountPerDivision(const SchoolModel& m,
    const ModelIndex& ix) {
    std::vector<int64_t> counts(m.divisions.size(), 0);
    for (size_t c = 0; c < m.divisions.size(); ++c) {
      // Streams = open groups x product of fixed-split sizes ("English 1/2
      // girls"). Mirrors BuildAtoms; keep the two in sync.
      int64_t fixed_tuples = 1;
      for (int s : ix.SplitsOfDivision(static_cast<int>(c))) {
        if (ix.SplitIsFixed(s) && !ix.GroupsOfSplit(s).empty()) {
          fixed_tuples *= static_cast<int64_t>(ix.GroupsOfSplit(s).size());
        }
      }
      int64_t open_groups = 0;
      for (int g : ix.GroupsOfDivision(static_cast<int>(c))) {
        if (!ix.SplitIsFixed(ix.EffectiveSplitOf(g))) ++open_groups;
      }
      if (open_groups == 0 && fixed_tuples == 1) continue;  // 0 = whole-class
      counts[c] = open_groups > 0 ? open_groups * fixed_tuples : fixed_tuples;
    }
    return counts;
  }

}  // namespace arrango
