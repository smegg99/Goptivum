// src/service/convert_model_to.cc

#include "service/convert.h"
#include "service/convert_common.h"

namespace arrango {
  namespace {

    v1::CountSource CountSourceToProto(CountSource s) {
      switch (s) {
      case CountSource::kImported:
        return v1::COUNT_SOURCE_IMPORTED;
      case CountSource::kManual:
        return v1::COUNT_SOURCE_MANUAL;
      case CountSource::kDefault:
        return v1::COUNT_SOURCE_DEFAULT;
      case CountSource::kUnknown:
        break;
      }
      return v1::COUNT_SOURCE_UNKNOWN;
    }

    v1::ExternalBlock::Target TargetToProto(BlockTarget t) {
      switch (t) {
      case BlockTarget::kDivision:
        return v1::ExternalBlock::TARGET_DIVISION;
      case BlockTarget::kGroup:
        return v1::ExternalBlock::TARGET_GROUP;
      case BlockTarget::kTeacher:
        return v1::ExternalBlock::TARGET_TEACHER;
      case BlockTarget::kRoom:
        return v1::ExternalBlock::TARGET_ROOM;
      }
      return v1::ExternalBlock::TARGET_UNSPECIFIED;
    }

  }  // namespace

  void ModelToProto(const SchoolModel& in, v1::SchoolModel* out) {
    out->set_name(in.name);
    for (const auto& d : in.days) {
      v1::Day* pd = out->add_days();
      pd->set_id(d.id);
      pd->set_name(d.name);
      pd->set_period_count(d.period_count);
    }
    for (const auto& p : in.periods) {
      v1::Period* pp = out->add_periods();
      pp->set_id(p.id);
      pp->set_name(p.name);
    }
    for (const auto& y : in.years) {
      v1::Year* py = out->add_years();
      py->set_id(y.id);
      py->set_name(y.name);
      py->set_level(y.level);
      py->set_priority(y.priority);
    }
    for (const auto& c : in.divisions) {
      v1::Division* pc = out->add_divisions();
      pc->set_id(c.id);
      pc->set_name(c.name);
      pc->set_year_id(c.year_id);
      pc->set_student_count(c.student_count);
      pc->set_count_source(CountSourceToProto(c.count_source));
      pc->set_source_ref(c.source_ref);
    }
    for (const auto& s : in.splits) {
      v1::Split* ps = out->add_splits();
      ps->set_id(s.id);
      ps->set_name(s.name);
      ps->set_division_id(s.division_id);
      ps->set_kind(s.kind == SplitKind::kFixed ? v1::SPLIT_KIND_FIXED
                                               : v1::SPLIT_KIND_OPEN);
    }
    for (const auto& g : in.groups) {
      v1::Group* pg = out->add_groups();
      pg->set_id(g.id);
      pg->set_name(g.name);
      pg->set_division_id(g.division_id);
      pg->set_student_count(g.student_count);
      pg->set_count_source(CountSourceToProto(g.count_source));
      pg->set_source_ref(g.source_ref);
      pg->set_split_id(g.split_id);
    }
    for (const auto& t : in.teachers) {
      v1::Teacher* pt = out->add_teachers();
      pt->set_id(t.id);
      pt->set_name(t.name);
      pt->set_source_ref(t.source_ref);
    }
    for (const auto& s : in.subjects) {
      v1::Subject* ps = out->add_subjects();
      ps->set_id(s.id);
      ps->set_name(s.name);
      ps->set_prefers_blocks(s.prefers_blocks);
      ps->set_source_ref(s.source_ref);
    }
    for (const auto& r : in.rooms) {
      v1::Room* pr = out->add_rooms();
      pr->set_id(r.id);
      pr->set_name(r.name);
      pr->set_designator(r.designator);
      pr->set_capacity(r.capacity);
      pr->set_capacity_source(CountSourceToProto(r.capacity_source));
      pr->set_source_ref(r.source_ref);
    }
    for (const auto& l : in.lessons) {
      v1::LessonInstance* pl = out->add_lessons();
      pl->set_id(l.id);
      for (const Participant& p : l.participants) {
        v1::Participant* pp = pl->add_participants();
        pp->set_division_id(p.division_id);
        pp->set_group_id(p.group_id);
      }
      pl->set_subject_id(l.subject_id);
      pl->set_teacher_id(l.teacher_id);
      pl->set_duration(l.duration);
      for (const std::string& d : l.allowed_room_designators) {
        pl->add_allowed_room_designators(d);
      }
      for (const std::string& d : l.disallowed_room_designators) {
        pl->add_disallowed_room_designators(d);
      }
      for (const std::string& d : l.observed_room_designators) {
        pl->add_observed_room_designators(d);
      }
      pl->set_fixed_room_id(l.fixed_room_id);
      pl->set_edge(l.edge == EdgePlacement::kFirst ? v1::EDGE_FIRST
        : l.edge == EdgePlacement::kLast ? v1::EDGE_LAST
        : l.edge == EdgePlacement::kEither ? v1::EDGE_EITHER
        : v1::EDGE_NONE);
      pl->set_parallel_block_id(l.parallel_block_id);
      pl->set_requires_teacher(l.requires_teacher);
      pl->set_requires_room(l.requires_room);
      pl->set_locked(l.locked);
      if (l.locked_placement) {
        PlacementToProto(*l.locked_placement, pl->mutable_locked_placement());
      }
      pl->set_has_previous(l.previous_placement.has_value());
      if (l.previous_placement) {
        PlacementToProto(*l.previous_placement, pl->mutable_previous_placement());
      }
    }
    for (const auto& b : in.external_blocks) {
      v1::ExternalBlock* pb = out->add_external_blocks();
      pb->set_id(b.id);
      pb->set_name(b.name);
      pb->set_target(TargetToProto(b.target));
      pb->set_target_id(b.target_id);
      pb->set_day_id(b.day_id);
      pb->set_start_period(b.start_period);
      pb->set_duration(b.duration);
    }
    v1::Weights* w = out->mutable_weights();
    w->set_student_gap_base(in.weights.student_gap_base);
    w->set_teacher_gap_base(in.weights.teacher_gap_base);
    w->set_late_student_lesson_base(in.weights.late_student_lesson_base);
    w->set_late_teacher_finish_base(in.weights.late_teacher_finish_base);
    w->set_subject_split_base(in.weights.subject_split_base);
    w->set_block_break_base(in.weights.block_break_base);
    w->set_room_change_base(in.weights.room_change_base);
    w->set_stability_move_base(in.weights.stability_move_base);
    w->set_expected_bad_per_lesson(in.weights.expected_bad_per_lesson);
    w->set_late_threshold_period(in.weights.late_threshold_period);
    w->set_gap_cap_per_day(in.weights.gap_cap_per_day);
    w->set_gap_window_base(in.weights.gap_window_base);
    w->set_early_start_base(in.weights.early_start_base);
    w->set_single_lesson_day_base(in.weights.single_lesson_day_base);
    RuleConfigToProto(in.rule_config, out->mutable_rule_config());
    for (const LessonLink& link : in.lesson_links) {
      v1::LessonLink* pl = out->add_lesson_links();
      pl->set_id(link.id);
      pl->set_kind(link.kind == LessonLinkKind::kSameDay
        ? v1::LESSON_LINK_SAME_DAY
        : link.kind == LessonLinkKind::kDifferentDay
        ? v1::LESSON_LINK_DIFFERENT_DAY
        : link.kind == LessonLinkKind::kConsecutive
        ? v1::LESSON_LINK_CONSECUTIVE
        : v1::LESSON_LINK_UNSPECIFIED);
      for (Id lesson : link.lesson_ids) pl->add_lesson_ids(lesson);
      pl->set_ordered(link.ordered);
    }
    for (const auto& p : in.preferences) {
      v1::Preference* pp = out->add_preferences();
      pp->set_kind(p.kind == PreferenceKind::kPreferEarly
        ? v1::Preference::KIND_PREFER_EARLY
        : v1::Preference::KIND_MAX_LESSONS_PER_DAY);
      pp->set_year_id(p.year_id);
      pp->set_division_id(p.division_id);
      pp->set_subject_id(p.subject_id);
      pp->set_weight(p.weight);
      pp->set_param(p.param);
    }
    for (const auto& r : in.daily_load_rules) {
      v1::DailyLoadRule* pr = out->add_daily_load_rules();
      pr->set_division_id(r.division_id);
      pr->set_group_id(r.group_id);
      pr->set_min_per_day(r.min_per_day);
      pr->set_max_per_day(r.max_per_day);
      pr->set_target_per_day(r.target_per_day);
      pr->set_allowed_deviation(r.allowed_deviation);
      pr->set_deviation_weight(r.deviation_weight);
      pr->set_imbalance_weight(r.imbalance_weight);
      pr->set_overload_weight(r.overload_weight);
      pr->set_underload_weight(r.underload_weight);
    }
  }

}  // namespace arrango
