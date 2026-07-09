// src/model/atoms.h

#pragma once

#include <string>
#include <vector>

#include "model/index.h"
#include "model/model.h"

namespace arrango {

  // An atom is the finest set of students the model can distinguish (all of
  // them always move together). Construction follows the split rulebook
  // (model/structure.h): one atom per (open group x fixed-split tuple), e.g.
  // "1/2 of the language split, girls". Divisions without open groups get one
  // atom per fixed tuple; without any groups, one whole-class atom. A
  // whole-division participant covers all of its division's atoms.
  struct Atom {
    int division_idx{};
    std::string label;  // e.g. "3aT", "3aT[1/2]", "3aT[1/2][girls]"
  };

  struct AtomSet {
    std::vector<Atom> atoms;
    std::vector<std::vector<int>> of_division;  // division_idx -> atom idxs
    std::vector<std::vector<int>> of_group;     // group_idx -> sorted atom idxs
    std::vector<std::vector<int>> of_lesson;    // lesson_idx -> sorted atom idxs
  };

  AtomSet BuildAtoms(const SchoolModel& m, const ModelIndex& ix);

}  // namespace arrango
