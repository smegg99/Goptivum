// src/model/streams.h

#pragma once

#include <string>
#include <vector>

#include "model/atoms.h"
#include "model/index.h"
#include "model/model.h"

namespace arrango {

  // A student stream is the unit for student-facing occupancy: one atom (a
  // maximal set of students that always move together). Every student belongs
  // to exactly one stream, so a lesson occupies a stream iff its atom set
  // contains the stream's atom — this makes "no two lessons overlap if their
  // student sets intersect" exact, cross-split and cross-division.
  struct StudentStream {
    int division_idx{};
    int atom_idx{};
    std::string label;
  };

  std::vector<StudentStream> BuildStudentStreams(const SchoolModel& m,
    const ModelIndex& ix,
    const AtomSet& atoms);

}  // namespace arrango
