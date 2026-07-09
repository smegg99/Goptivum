// src/model/streams.cc

#include "model/streams.h"

namespace arrango {

  std::vector<StudentStream> BuildStudentStreams(const SchoolModel& m,
    const ModelIndex& ix,
    const AtomSet& atoms) {
    (void)m;
    (void)ix;  // reserved: named streams may need model data beyond the atoms
    std::vector<StudentStream> streams;
    streams.reserve(atoms.atoms.size());
    for (size_t a = 0; a < atoms.atoms.size(); ++a) {
      streams.push_back({ atoms.atoms[a].division_idx, static_cast<int>(a),
                         atoms.atoms[a].label });
    }
    return streams;
  }

}  // namespace arrango
