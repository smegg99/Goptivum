// src/score/rules.cc

#include "score/rules.h"

#include "score/penalty_defs.h"

namespace arrango {
  namespace {

    // Named profiles = unscoped override lists applied onto the defaults.
    // dobry_plan mirrors their iron invariants (docs/research notes).
    const std::vector<RuleOverride>* ProfileOverrides(
      const std::string& profile) {
      static const std::vector<RuleOverride> kEmpty;
      static const std::vector<RuleOverride> kDobryPlan = {
        { "student_gaps", RuleMode::kHard },
        { "gap_windows", RuleMode::kHard },
        { "subject_once", RuleMode::kHard },
        { "anti_split_shift", RuleMode::kSoft, /*weight=*/60, /*param=*/2 },
      };
      static const std::vector<RuleOverride> kRelaxed = {
        { "gap_windows", RuleMode::kSoft },
        { "student_gaps", RuleMode::kSoft, /*weight=*/5000 },
      };
      if (profile.empty() || profile == "default") return &kEmpty;
      if (profile == "dobry_plan") return &kDobryPlan;
      if (profile == "relaxed") return &kRelaxed;
      return nullptr;  // unknown -> diagnostic
    }

  }  // namespace

  const std::vector<RuleDef>& RuleTable() {
    // Column order: key, categories, metric, default mode, hardable,
    // by_year, by_division, by_subject, by_teacher.
    static const std::vector<RuleDef> table = {
      { "student_gaps", { kCatStudentGap }, "gaps",
        RuleMode::kSoft, true, true, true, false, false },
      { "gap_windows", { kCatGapWindow }, "gap_windows",
        RuleMode::kHard, true, true, true, false, false },
      { "subject_once", { kCatSubjectSplit, kCatBlockBreak }, "repeats",
        RuleMode::kSoft, true, true, true, true, false },
      { "late_student", { kCatLateStudent }, "lateness",
        RuleMode::kSoft, true, true, true, true, false },
      { "late_teacher", { kCatLateTeacher }, "t_late",
        RuleMode::kSoft, true, false, false, false, true },
      { "teacher_gaps", { kCatTeacherGap }, "t_gaps",
        RuleMode::kSoft, true, false, false, false, true },
      { "room_changes", { kCatRoomChange }, "room_moves",
        RuleMode::kSoft, true, false, false, false, true },
      // Subject scope means EXEMPTION: a subject-scoped `off` override
      // excludes that subject's periods from the daily count ("przedmiot
      // extra"), it does not disable the rule.
      { "max_lessons", { kCatMaxLessons }, "overload",
        RuleMode::kSoft, true, true, true, true, false },
      { "daily_band",
        { kCatDailyDeviation, kCatDailyOverload, kCatDailyUnderload },
        "load_band", RuleMode::kSoft, true, false, true, false, false },
      { "daily_imbalance", { kCatDailyImbalance }, "",
        RuleMode::kSoft, true, false, true, false, false },
      { "single_lesson_day", { kCatSingleLessonDay }, "single_days",
        RuleMode::kSoft, true, false, false, false, true },
      { "anti_split_shift", { kCatAntiSplitShift }, "",
        RuleMode::kOff, true, false, false, false, true },
      { "teach_daily", { kCatTeachDaily }, "",
        RuleMode::kOff, true, false, false, false, true },
      { "few_days", { kCatFewDays }, "",
        RuleMode::kOff, true, false, false, false, true },
      { "stability", { kCatStability }, "",
        RuleMode::kSoft, false, false, true, false, false },
      { "prefer_early", { kCatPreferEarly }, "",
        RuleMode::kSoft, false, true, true, true, false },
    };
    return table;
  }

  int RuleResolver::RuleIndex(std::string_view key) {
    const std::vector<RuleDef>& table = RuleTable();
    for (size_t i = 0; i < table.size(); ++i) {
      if (key == table[i].key) return static_cast<int>(i);
    }
    return -1;
  }

  RuleResolver RuleResolver::Build(const SchoolModel& m, const ModelIndex& ix,
    const RuleConfig& request) {
    RuleResolver r;
    const std::vector<RuleDef>& table = RuleTable();
    r.base_.resize(table.size());
    r.layers_.resize(table.size());
    for (size_t i = 0; i < table.size(); ++i) {
      r.base_[i] = { table[i].default_mode, 0, 0 };
    }

    // One pass per layer, in precedence order. Profiles are unscoped, so
    // they fold straight into base_; scoped lists become ordered layers.
    auto apply_list = [&] (const std::vector<RuleOverride>& list,
      const char* origin, bool into_base) {
      for (const RuleOverride& o : list) {
        const int i = RuleIndex(o.rule);
        if (i < 0) {
          r.diagnostics_.push_back(std::string(origin) +
            " names unknown rule '" + o.rule + "'");
          continue;
        }
        const RuleDef& def = table[i];
        if (o.mode == RuleMode::kHard && !def.hardable) {
          r.diagnostics_.push_back("rule '" + o.rule +
            "' cannot be hard (" + std::string(origin) + ")");
          continue;
        }
        // Scope sanity: the dimension must be supported and the id known.
        struct Dim { Id id; bool allowed; const char* name; int idx; };
        const Dim dims[] = {
          { o.year_id, def.by_year, "year", ix.YearIdx(o.year_id) },
          { o.division_id, def.by_division, "division",
            ix.DivisionIdx(o.division_id) },
          { o.subject_id, def.by_subject, "subject",
            ix.SubjectIdx(o.subject_id) },
          { o.teacher_id, def.by_teacher, "teacher",
            ix.TeacherIdx(o.teacher_id) },
        };
        bool bad = false;
        for (const Dim& d : dims) {
          if (d.id == kNoId) continue;
          if (!d.allowed) {
            r.diagnostics_.push_back("rule '" + o.rule + "' does not take " +
              d.name + " overrides (" + origin + ")");
            bad = true;
          }
          else if (d.idx < 0) {
            r.diagnostics_.push_back("rule '" + o.rule +
              "' override names unknown " + d.name + " id " +
              std::to_string(d.id) + " (" + origin + ")");
            bad = true;
          }
        }
        if (bad) continue;

        if (into_base) {
          ResolvedRule& base = r.base_[i];
          if (o.mode != RuleMode::kDefault) base.mode = o.mode;
          if (o.weight > 0) base.weight = o.weight;
          if (o.param > 0) base.param = o.param;
        }
        else {
          r.layers_[i].push_back({ o.mode, o.weight, o.param, o.year_id,
                                  o.division_id, o.subject_id, o.teacher_id });
        }
      }
      };

    const std::vector<RuleOverride>* profile =
      ProfileOverrides(m.rule_config.profile);
    if (profile == nullptr) {
      r.diagnostics_.push_back("unknown rule profile '" +
        m.rule_config.profile + "'");
    }
    else {
      apply_list(*profile, "profile", /*into_base=*/true);
    }
    // Request-level profile (if any) layers over the model's.
    if (!request.profile.empty() && request.profile != m.rule_config.profile) {
      const std::vector<RuleOverride>* rp = ProfileOverrides(request.profile);
      if (rp == nullptr) {
        r.diagnostics_.push_back("unknown rule profile '" + request.profile +
          "' (request)");
      }
      else {
        apply_list(*rp, "request profile", /*into_base=*/true);
      }
    }
    apply_list(m.rule_config.overrides, "model override",
      /*into_base=*/false);
    apply_list(request.overrides, "request override", /*into_base=*/false);
    return r;
  }

  ResolvedRule RuleResolver::For(std::string_view rule, Id year, Id division,
    Id subject, Id teacher) const {
    const int i = RuleIndex(rule);
    if (i < 0) return {};  // callers pass table keys; keep total anyway
    ResolvedRule resolved = base_[i];
    for (const Layer& layer : layers_[i]) {
      // A populated scope field must match the query; kNoId matches all.
      if (layer.year != kNoId && layer.year != year) continue;
      if (layer.division != kNoId && layer.division != division) continue;
      if (layer.subject != kNoId && layer.subject != subject) continue;
      if (layer.teacher != kNoId && layer.teacher != teacher) continue;
      if (layer.mode != RuleMode::kDefault) resolved.mode = layer.mode;
      if (layer.weight > 0) resolved.weight = layer.weight;
      if (layer.param > 0) resolved.param = layer.param;
    }
    return resolved;
  }

  ResolvedRule RuleResolver::SchoolWide(std::string_view rule) const {
    return For(rule, kNoId, kNoId, kNoId, kNoId);
  }

}  // namespace arrango
