// src/score/rules.h

#pragma once

#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

#include "model/index.h"
#include "model/model.h"

namespace arrango {

  // THE rule table: one row per comfort rule, the single source that the
  // objective, scorer, verdict, preflight, and the baseline check all
  // derive from. Row defaults reproduce today's solver exactly, so a model
  // with no RuleConfig behaves byte-identically to the pre-framework code.
  struct RuleDef {
    const char* key;                       // stable API string
    std::vector<const char*> categories;   // penalty categories it produces
    const char* metric;                    // metric key, "" = not rated
    RuleMode default_mode;                 // today's behavior
    bool hardable;                         // false: kHard is a config error
    // Scope dimensions overrides may use (others are config errors):
    bool by_year, by_division, by_subject, by_teacher;
  };
  const std::vector<RuleDef>& RuleTable();

  // A rule resolved for one entity. weight/param 0 mean "use the built-in
  // formula" — term sites keep their existing weight logic unless a user
  // explicitly overrode it.
  struct ResolvedRule {
    RuleMode mode{ RuleMode::kSoft };  // never kDefault after resolution
    int64_t weight{};
    uint32_t param{};
  };

  // anti_split_shift: how many periods at each day edge count as
  // "early"/"late" when the rule's param is 0.
  inline constexpr uint32_t kAntiSplitShiftDefaultEdge = 2;

  // Layered resolution: table defaults -> named profile -> model overrides
  // -> request overrides. Within one list, purely last-match-wins. Config
  // mistakes (unknown rule/profile/scope, hard on a non-hardable rule) land
  // in Diagnostics() for the preflight to refuse with names.
  class RuleResolver {
  public:
    static RuleResolver Build(const SchoolModel& m, const ModelIndex& ix,
      const RuleConfig& request);

    // Any scope id may be kNoId when the dimension does not apply.
    ResolvedRule For(std::string_view rule, Id year, Id division, Id subject,
      Id teacher) const;
    // All-scopes-kNoId resolution: only unscoped overrides apply. Feeds the
    // config echo and the verdict's per-rule mode sets.
    ResolvedRule SchoolWide(std::string_view rule) const;

    // Borrows from this resolver — never call on a temporary
    // (Build(...).Diagnostics() dangles).
    const std::vector<std::string>& Diagnostics() const {
      return diagnostics_;
    }

  private:
    struct Layer {
      RuleMode mode;
      int64_t weight;
      uint32_t param;
      Id year, division, subject, teacher;
    };
    // rule key -> base (defaults + profile, school-wide) and the ordered
    // scoped layers (model overrides then request overrides).
    std::vector<ResolvedRule> base_;             // indexed like RuleTable()
    std::vector<std::vector<Layer>> layers_;     // indexed like RuleTable()
    std::vector<std::string> diagnostics_;

    static int RuleIndex(std::string_view key);
  };

}  // namespace arrango
