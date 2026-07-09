// src/model/model.h

#pragma once

#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "model/ids.h"

namespace arrango {

  // Canonical flat model. Mirrors proto/arrango/v1/school.proto one-to-one.
  // No nested object graphs: everything references by Id.

  struct Day {
    Id id{};
    std::string name;
    uint32_t period_count{};
    bool operator==(const Day&) const = default;
  };

  // Order in SchoolModel::periods defines the global period index.
  struct Period {
    Id id{};
    std::string name;
    bool operator==(const Period&) const = default;
  };

  struct Year {
    Id id{};
    std::string name;
    uint32_t level{};
    // Student-facing penalty multiplier, percent (100 = neutral).
    uint32_t priority{ 100 };
    bool operator==(const Year&) const = default;
  };

  // Where a stored count (division/group size, room capacity) came from.
  // kUnknown means "no usable value" — the count field must be ignored.
  enum class CountSource { kUnknown, kImported, kManual, kDefault };

  // Whether a split's membership is decided around the plan (open) or is a
  // fact of the world (fixed). See Split.
  enum class SplitKind { kOpen, kFixed };

  // One physical way of cutting a division into groups ("languages", "PE",
  // "workshop"). Split membership is the ONLY source of student-disjointness
  // knowledge: different groups of one split never share students; groups of
  // different splits share students unless both splits are open.
  struct Split {
    Id id{};
    std::string name;
    Id division_id{};
    SplitKind kind{ SplitKind::kOpen };
    bool operator==(const Split&) const = default;
  };

  struct Division {
    Id id{};
    std::string name;
    Id year_id{};
    // Meaningful only when count_source != kUnknown.
    uint32_t student_count{};
    CountSource count_source{ CountSource::kUnknown };
    // Opaque importer-defined provenance (Optivum: "o20"). Never interpreted.
    std::string source_ref;
    bool operator==(const Division&) const = default;
  };

  struct Group {
    Id id{};
    std::string name;
    Id division_id{};
    uint32_t student_count{};
    CountSource count_source{ CountSource::kUnknown };
    std::string source_ref;
    // Owning split; kNoId = implicit private OPEN split (permissive default).
    Id split_id{ kNoId };
    bool operator==(const Group&) const = default;
  };

  struct Teacher {
    Id id{};
    std::string name;
    std::string source_ref;
    bool operator==(const Teacher&) const = default;
  };

  struct Subject {
    Id id{};
    std::string name;
    bool prefers_blocks{ false };
    std::string source_ref;
    bool operator==(const Subject&) const = default;
  };

  struct Room {
    Id id{};
    std::string name;  // full display title
    // Canonical short room code ("106", "pe3", "SKat"); exact-match
    // semantics.
    std::string designator;
    // Meaningful only when capacity_source != kUnknown.
    uint32_t capacity{};
    CountSource capacity_source{ CountSource::kUnknown };
    std::string source_ref;
    bool operator==(const Room&) const = default;
  };

  // One student-set taking part in a lesson. kNoId group = whole division.
  struct Participant {
    Id division_id{};
    Id group_id{ kNoId };
    bool operator==(const Participant&) const = default;
  };

  struct Placement {
    Id day_id{ kNoId };
    uint32_t start_period{};
    Id room_id{ kNoId };
    bool operator==(const Placement&) const = default;
  };

  // Relative-placement constraint between 2+ lessons. HARD by design — a
  // link you want soft is a preference, and none of these has a sensible
  // soft meaning.
  enum class LessonLinkKind {
    kUnspecified, kSameDay, kDifferentDay,
    kConsecutive
  };

  struct LessonLink {
    Id id{ kNoId };
    LessonLinkKind kind{ LessonLinkKind::kUnspecified };
    std::vector<Id> lesson_ids;  // 2+ members, no duplicates
    bool ordered{ false };       // kConsecutive: follow list order in time
    bool operator==(const LessonLink&) const = default;
  };

  // Edge-of-day placement, relative to every student stream containing the
  // lesson (the teacher is irrelevant).
  enum class EdgePlacement { kNone, kFirst, kLast, kEither };

  // One concrete schedulable unit (not a weekly subject count).
  struct LessonInstance {
    Id id{};
    // At least one; all participants attend together (merged groups
    // allowed, including across divisions).
    std::vector<Participant> participants;
    Id subject_id{};
    Id teacher_id{ kNoId };  // may be kNoId only when !requires_teacher
    uint32_t duration{ 1 };
    // Empty = unrestricted (any room). Disallowed always wins.
    std::vector<std::string> allowed_room_designators;
    std::vector<std::string> disallowed_room_designators;
    // Informational: designators seen for this subject in imports.
    std::vector<std::string> observed_room_designators;
    Id fixed_room_id{ kNoId };  // explicit exact-room lock
    Id parallel_block_id{ kNoId };
    bool requires_teacher{ true };
    bool requires_room{ true };
    bool locked{ false };
    std::optional<Placement> locked_placement;
    // Stability hint only; never feeds room eligibility.
    std::optional<Placement> previous_placement;
    // Must be its STUDENTS' first and/or last lesson of the day.
    EdgePlacement edge{ EdgePlacement::kNone };
    bool operator==(const LessonInstance&) const = default;
  };

  // Per-day lesson-load rule. kNoId division = school default;
  // group scope wins over division scope wins over default.
  // min/max are HARD whole-division bounds; the four weights are SOFT
  // per-stream penalties against target_per_day (see penalty_defs.h for the
  // exact semantics; a day's load includes external blocks, weights <= 0 are
  // off, student penalties scale with year priority).
  struct DailyLoadRule {
    Id division_id{ kNoId };
    Id group_id{ kNoId };
    uint32_t min_per_day{};
    uint32_t max_per_day{};     // 0 = no maximum
    uint32_t target_per_day{};  // 0 = auto: ceil(weekly / active days)
    uint32_t allowed_deviation{};  // tolerated |load - target| before penalties
    int64_t deviation_weight{};    // per period beyond target +/- allowed
    int64_t imbalance_weight{};    // per period of (max - min) load across days
    int64_t overload_weight{};     // per period above target + allowed
    int64_t underload_weight{};    // per period below target - allowed
    bool operator==(const DailyLoadRule&) const = default;
  };

  enum class BlockTarget { kDivision, kGroup, kTeacher, kRoom };

  // Fixed occupied/unavailable time for exactly one target entity.
  struct ExternalBlock {
    Id id{};
    std::string name;
    BlockTarget target{};
    Id target_id{};
    Id day_id{};
    uint32_t start_period{};
    uint32_t duration{ 1 };
    bool operator==(const ExternalBlock&) const = default;
  };

  struct Weights {
    int64_t student_gap_base{ 10000 };  // near-hard: one gap outweighs other prefs
    int64_t teacher_gap_base{ 45 };
    int64_t late_student_lesson_base{ 40 };
    int64_t late_teacher_finish_base{ 20 };
    int64_t subject_split_base{ 70 };
    int64_t block_break_base{ 90 };
    int64_t room_change_base{ 10 };
    int64_t stability_move_base{ 15 };
    // VESTIGE: fed the old penalty-based quality formula; the absolute
    // metric rating (score/metrics.h) ignores it. Kept for proto compat.
    int64_t expected_bad_per_lesson{ 60 };
    uint32_t late_threshold_period{ 7 };  // 0-based period index
    uint32_t gap_cap_per_day{ 3 };  // caps TEACHER gaps only; student gaps are uncapped
    // Escalation per ADJACENT pair of gap periods (multi-period windows are
    // "even harder" than single gaps: a 3-period window costs 3 gaps + 2 of
    // these).
    int64_t gap_window_base{ 100000 };
    // Per-period cost of starting later in the day; 0 = preference off.
    int64_t early_start_base{ 0 };
    // Per teacher-day holding exactly one lesson; 0 = preference off.
    int64_t single_lesson_day_base{ 0 };
    bool operator==(const Weights&) const = default;
  };

  enum class PreferenceKind { kPreferEarly, kMaxLessonsPerDay };

  struct Preference {
    PreferenceKind kind{};
    Id year_id{ kNoId };    // filters; kNoId = matches any
    Id division_id{ kNoId };
    Id subject_id{ kNoId };
    int64_t weight{};
    uint32_t param{};
    bool operator==(const Preference&) const = default;
  };

  // Rule modes: every comfort rule has a three-position switch. kDefault in
  // an override means "keep whatever the earlier layer decided".
  enum class RuleMode { kDefault, kHard, kSoft, kOff };

  // One override, applied on top of the built-in defaults and the profile.
  // Empty scope fields = school-wide; a populated field must match the
  // entity being resolved. Within a list, the LAST matching override wins —
  // there is no implicit specificity ranking.
  struct RuleOverride {
    std::string rule;   // key from score/rules.h RuleTable()
    RuleMode mode{ RuleMode::kDefault };
    int64_t weight{};   // soft weight; 0 = keep the built-in formula
    uint32_t param{};   // rule-specific (anti_split_shift edge periods)
    Id year_id{ kNoId };
    Id division_id{ kNoId };
    Id subject_id{ kNoId };
    Id teacher_id{ kNoId };
    bool operator==(const RuleOverride&) const = default;
  };

  // A school's rule philosophy: a named profile applied first, then the
  // overrides in order. Lives on the model because it IS school data.
  struct RuleConfig {
    std::string profile;  // "", "default", "dobry_plan", "relaxed"
    std::vector<RuleOverride> overrides;
    bool operator==(const RuleConfig&) const = default;
  };

  struct SchoolModel {
    std::string name;
    std::vector<Day> days;
    std::vector<Period> periods;
    std::vector<Year> years;
    std::vector<Division> divisions;
    std::vector<Group> groups;
    std::vector<Teacher> teachers;
    std::vector<Subject> subjects;
    std::vector<Room> rooms;
    std::vector<LessonInstance> lessons;
    std::vector<ExternalBlock> external_blocks;
    Weights weights;
    std::vector<Preference> preferences;
    std::vector<DailyLoadRule> daily_load_rules;
    std::vector<Split> splits;
    RuleConfig rule_config;
    std::vector<LessonLink> lesson_links;
    bool operator==(const SchoolModel&) const = default;
  };

  struct ScheduledLesson {
    Id lesson_id{};
    Placement placement;
    bool operator==(const ScheduledLesson&) const = default;
  };

  struct ScheduleSnapshot {
    std::vector<ScheduledLesson> lessons;
    bool operator==(const ScheduleSnapshot&) const = default;
  };

}  // namespace arrango
