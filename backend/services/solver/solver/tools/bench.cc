// tools/bench.cc

#include <cstdio>
#include <cstring>
#include <variant>
#include "demo/demo_data.h"
#include "model/index.h"
#include "score/scorer.h"
#include "solve/candidates.h"
#include "solve/solver.h"
#include "validate/validator.h"
using namespace arrango;

// Maps a CLI preset name to the enum; defaults to production.
static DemoPreset PresetByName(const char* name) {
  if (std::strcmp(name, "mega") == 0) return DemoPreset::kMega;
  if (std::strcmp(name, "production") == 0) return DemoPreset::kProduction;
  return DemoPreset::kProduction;
}

int main(int argc, char** argv) {
  // Usage: bench [seconds] [preset] [workers]
  double secs = argc > 1 ? atof(argv[1]) : 60.0;
  DemoPreset preset = argc > 2 ? PresetByName(argv[2]) : DemoPreset::kProduction;
  int workers = argc > 3 ? atoi(argv[3]) : 1;
  SchoolModel m = GenerateDemoSchool(preset, 42);
  std::printf("model: divisions=%zu groups=%zu teachers=%zu rooms=%zu "
    "subjects=%zu lessons=%zu\n",
    m.divisions.size(), m.groups.size(), m.teachers.size(),
    m.rooms.size(), m.subjects.size(), m.lessons.size());
  ModelIndex ix = ModelIndex::Build(m);
  auto cand = BuildCandidates(m, ix, UnknownCapacityPolicy::kAllow);
  if (std::holds_alternative<CandidateSet>(cand)) {
    std::printf("candidates: %zu\n", std::get<CandidateSet>(cand).all.size());
  }
  SolveParams p{ .max_time_seconds = secs, .num_workers = workers,
                 .random_seed = 7 };
  SolveResult r = SolveSchedule(m, p, nullptr, [] (const SolveResult& s) {
    std::printf("  improved: obj=%ld t=%.2f\n", s.objective, s.wall_time_seconds);
    });
  std::printf("final: status=%d obj=%ld time=%.2f\n", (int)r.status, r.objective, r.wall_time_seconds);
  if (!r.best.lessons.empty()) {
    auto score = ComputeScore(m, r.best);
    std::printf("quality=%.1f (students=%.1f teachers=%.1f) penalty=%ld\n",
      score.overall_quality, score.all_students_quality,
      score.all_teachers_quality, score.total_penalty);
    for (const PenaltyItem& it : score.global_items) {
      if (it.penalty > 0) {
        std::printf("  %-16s penalty=%-12ld count=%u\n", it.category.c_str(),
          it.penalty, it.count);
      }
    }
    // Independent hard-validity cross-check.
    ValidationReport vr = Validate(m, r.best);
    int hard = 0;
    for (const Conflict& c : vr.conflicts) (void)c, ++hard;
    std::printf("validator: %d hard conflict(s), placed=%zu/%zu\n", hard,
      r.best.lessons.size(), m.lessons.size());
  }
  return 0;
}
