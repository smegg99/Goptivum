// src/model/atoms.cc

#include "model/atoms.h"

#include <algorithm>

namespace arrango {

  AtomSet BuildAtoms(const SchoolModel& m, const ModelIndex& ix) {
    AtomSet set;
    set.of_division.resize(m.divisions.size());
    set.of_group.assign(m.groups.size(), {});

    // Atoms mirror the split rulebook (model/structure.h): a student is
    // identified by their open group plus their group in every FIXED split,
    // so atoms are the product "open group x fixed tuple". With no splits
    // declared this degenerates to one atom per group — the legacy behavior.
    for (size_t c = 0; c < m.divisions.size(); ++c) {
      // All combinations of one group per fixed split, e.g. pe{girls,boys} x
      // lunch{1st,2nd} -> [girls,1st][girls,2nd][boys,1st][boys,2nd].
      // Starts as one empty tuple so "no fixed splits" yields a single
      // combination.
      std::vector<std::vector<int>> tuples{ {} };
      for (int s : ix.SplitsOfDivision(static_cast<int>(c))) {
        if (!ix.SplitIsFixed(s) || ix.GroupsOfSplit(s).empty()) continue;
        std::vector<std::vector<int>> extended;
        for (const std::vector<int>& tuple : tuples) {
          for (int g : ix.GroupsOfSplit(s)) {
            std::vector<int> next = tuple;
            next.push_back(g);
            extended.push_back(std::move(next));
          }
        }
        tuples = std::move(extended);
      }
      // Open groups (including split-less ones = implicit private open).
      std::vector<int> open_groups;
      for (int g : ix.GroupsOfDivision(static_cast<int>(c))) {
        if (!ix.SplitIsFixed(ix.EffectiveSplitOf(g))) open_groups.push_back(g);
      }

      auto add_atom = [&] (int open_group, const std::vector<int>& tuple) {
        const int atom_idx = static_cast<int>(set.atoms.size());
        std::string label = m.divisions[c].name;
        if (open_group >= 0) label += "[" + m.groups[open_group].name + "]";
        for (int fg : tuple) label += "[" + m.groups[fg].name + "]";
        set.atoms.push_back({ static_cast<int>(c), std::move(label) });
        set.of_division[c].push_back(atom_idx);
        // of_group stays sorted: atoms are appended in increasing index order.
        if (open_group >= 0) set.of_group[open_group].push_back(atom_idx);
        for (int fg : tuple) set.of_group[fg].push_back(atom_idx);
        };

      if (open_groups.empty()) {
        // No open groups: one atom per fixed tuple (or the single empty
        // tuple = one whole-class atom for a group-less division).
        for (const std::vector<int>& tuple : tuples) add_atom(-1, tuple);
      }
      else {
        for (int g : open_groups) {
          for (const std::vector<int>& tuple : tuples) add_atom(g, tuple);
        }
      }
    }

    // Lesson -> union of its participants' atoms.
    set.of_lesson.assign(m.lessons.size(), {});
    for (size_t li = 0; li < m.lessons.size(); ++li) {
      std::vector<int>& out = set.of_lesson[li];
      for (const Participant& p : m.lessons[li].participants) {
        int c = ix.DivisionIdx(p.division_id);
        if (c < 0) continue;
        if (p.group_id == kNoId) {
          out.insert(out.end(), set.of_division[c].begin(),
            set.of_division[c].end());
        }
        else {
          int g = ix.GroupIdx(p.group_id);
          if (g >= 0) {
            out.insert(out.end(), set.of_group[g].begin(),
              set.of_group[g].end());
          }
        }
      }
      std::sort(out.begin(), out.end());
      out.erase(std::unique(out.begin(), out.end()), out.end());
    }

    return set;
  }

}  // namespace arrango
