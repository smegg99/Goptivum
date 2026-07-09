// src/score/verdict.cc

#include "score/verdict.h"

#include <set>
#include <string>
#include <string_view>

#include "score/penalty_defs.h"
#include "score/rules.h"

namespace arrango {
  namespace {

    // The pristine bar (user decision 2026-07-08): every comfort category —
    // student and teacher — blocks TIER_PRISTINE. Preference-flavored
    // categories (stability, prefer_early) and INFO findings do not.
    // Derived from the rule table so the two can never drift; OFF-mode
    // rules never reach here — the scorer emits no issues for them.
    const std::set<std::string>& WarningCategories() {
      static const std::set<std::string> designated = [] {
        std::set<std::string> cats;
        for (const RuleDef& rule : RuleTable()) {
          if (std::string_view(rule.key) == "stability" ||
            std::string_view(rule.key) == "prefer_early") {
            continue;  // preferences, not the comfort bar
          }
          for (const char* category : rule.categories) cats.insert(category);
        }
        return cats;
      }();
      return designated;
    }

    // INFO categories (hygiene detectors) — counted, never tier-changing.
    const std::set<std::string>& InfoCategories() {
      static const std::set<std::string> infos = {
        kCatStartVariance, kCatLateFirstLesson, kCatSubjectAlwaysLast,
      };
      return infos;
    }

  }  // namespace

  ScheduleVerdict ComposeVerdict(const ValidationReport& validation,
    const ScoreReport& score) {
    ScheduleVerdict verdict;
    verdict.errors = static_cast<uint32_t>(validation.conflicts.size());
    for (const SoftIssue& issue : score.soft_issues) {
      if (issue.count == 0) continue;
      if (issue.config_hard) {
        // A violation of a rule the school set to HARD: an error, not a
        // warning — and counted separately so clients can tell it apart
        // from structural illegality.
        ++verdict.config_hard_errors;
        ++verdict.errors;
      }
      else if (WarningCategories().count(issue.category)) ++verdict.warnings;
      else if (InfoCategories().count(issue.category)) ++verdict.infos;
    }
    for (const SoftIssue& issue : score.info_issues) {
      if (InfoCategories().count(issue.category)) ++verdict.infos;
    }
    verdict.tier = verdict.errors > 0 ? VerdictTier::kErrors
      : verdict.warnings > 0 ? VerdictTier::kWarnings
      : VerdictTier::kPristine;
    return verdict;
  }

}  // namespace arrango
