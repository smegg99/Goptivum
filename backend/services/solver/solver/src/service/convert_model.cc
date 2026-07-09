// src/service/convert_model.cc

#include "service/convert.h"
#include "service/convert_common.h"

namespace arrango {
  namespace {

    CountSource CountSourceFromProto(v1::CountSource s) {
      switch (s) {
      case v1::COUNT_SOURCE_IMPORTED:
        return CountSource::kImported;
      case v1::COUNT_SOURCE_MANUAL:
        return CountSource::kManual;
      case v1::COUNT_SOURCE_DEFAULT:
        return CountSource::kDefault;
      default:
        return CountSource::kUnknown;
      }
    }

    BlockTarget TargetFromProto(v1::ExternalBlock::Target t) {
      switch (t) {
      case v1::ExternalBlock::TARGET_GROUP:
        return BlockTarget::kGroup;
      case v1::ExternalBlock::TARGET_TEACHER:
        return BlockTarget::kTeacher;
      case v1::ExternalBlock::TARGET_ROOM:
        return BlockTarget::kRoom;
      default:
        return BlockTarget::kDivision;
      }
    }

  }  // namespace

  SchoolModel ModelFromProto(const v1::SchoolModel& in) {
    SchoolModel m;
    m.name = in.name();
    for (const auto& d : in.days()) {
      m.days.push_back({ d.id(), d.name(), d.period_count() });
    }
    for (const auto& p : in.periods()) m.periods.push_back({ p.id(), p.name() });
    for (const auto& y : in.years()) {
      m.years.push_back({ y.id(), y.name(), y.level(), y.priority() });
    }
    for (const auto& c : in.divisions()) {
      m.divisions.push_back({ c.id(), c.name(), c.year_id(), c.student_count(),
                             CountSourceFromProto(c.count_source()),
                             c.source_ref() });
    }
    for (const auto& s : in.splits()) {
      m.splits.push_back({ s.id(), s.name(), s.division_id(),
                          s.kind() == v1::SPLIT_KIND_FIXED
                            ? SplitKind::kFixed
                            : SplitKind::kOpen });
    }
    for (const auto& g : in.groups()) {
      m.groups.push_back({ g.id(), g.name(), g.division_id(),
                          g.student_count(),
                          CountSourceFromProto(g.count_source()),
                          g.source_ref(), g.split_id() });
    }
    for (const auto& t : in.teachers()) {
      m.teachers.push_back({ t.id(), t.name(), t.source_ref() });
    }
    for (const auto& s : in.subjects()) {
      m.subjects.push_back({ s.id(), s.name(), s.prefers_blocks(),
                            s.source_ref() });
    }
    for (const auto& r : in.rooms()) {
      m.rooms.push_back({ r.id(), r.name(), r.designator(), r.capacity(),
                         CountSourceFromProto(r.capacity_source()),
                         r.source_ref() });
    }
    for (const auto& l : in.lessons()) {
      LessonInstance lesson;
      lesson.id = l.id();
      for (const auto& p : l.participants()) {
        lesson.participants.push_back({ p.division_id(), p.group_id() });
      }
      lesson.subject_id = l.subject_id();
      lesson.teacher_id = l.teacher_id();
      lesson.duration = l.duration();
      lesson.allowed_room_designators.assign(
        l.allowed_room_designators().begin(),
        l.allowed_room_designators().end());
      lesson.disallowed_room_designators.assign(
        l.disallowed_room_designators().begin(),
        l.disallowed_room_designators().end());
      lesson.observed_room_designators.assign(
        l.observed_room_designators().begin(),
        l.observed_room_designators().end());
      lesson.fixed_room_id = l.fixed_room_id();
      switch (l.edge()) {
      case v1::EDGE_FIRST: lesson.edge = EdgePlacement::kFirst; break;
      case v1::EDGE_LAST: lesson.edge = EdgePlacement::kLast; break;
      case v1::EDGE_EITHER: lesson.edge = EdgePlacement::kEither; break;
      default: lesson.edge = EdgePlacement::kNone; break;
      }
      lesson.parallel_block_id = l.parallel_block_id();
      lesson.requires_teacher = l.requires_teacher();
      lesson.requires_room = l.requires_room();
      lesson.locked = l.locked();
      if (l.locked()) {
        lesson.locked_placement = PlacementFromProto(l.locked_placement());
      }
      if (l.has_previous()) {
        lesson.previous_placement = PlacementFromProto(l.previous_placement());
      }
      m.lessons.push_back(std::move(lesson));
    }
    for (const auto& b : in.external_blocks()) {
      m.external_blocks.push_back({ b.id(), b.name(),
                                   TargetFromProto(b.target()), b.target_id(),
                                   b.day_id(), b.start_period(), b.duration() });
    }
    const v1::Weights& w = in.weights();
    m.weights = Weights{ w.student_gap_base(),        w.teacher_gap_base(),
                        w.late_student_lesson_base(), w.late_teacher_finish_base(),
                        w.subject_split_base(),      w.block_break_base(),
                        w.room_change_base(),        w.stability_move_base(),
                        w.expected_bad_per_lesson(), w.late_threshold_period(),
                        w.gap_cap_per_day(),         w.gap_window_base(),
                        w.early_start_base(),        w.single_lesson_day_base() };
    m.rule_config = RuleConfigFromProto(in.rule_config());
    for (const auto& link : in.lesson_links()) {
      LessonLinkKind kind = LessonLinkKind::kUnspecified;
      switch (link.kind()) {
      case v1::LESSON_LINK_SAME_DAY: kind = LessonLinkKind::kSameDay; break;
      case v1::LESSON_LINK_DIFFERENT_DAY:
        kind = LessonLinkKind::kDifferentDay; break;
      case v1::LESSON_LINK_CONSECUTIVE:
        kind = LessonLinkKind::kConsecutive; break;
      default: break;
      }
      LessonLink out{ link.id(), kind, {}, link.ordered() };
      for (Id lesson : link.lesson_ids()) out.lesson_ids.push_back(lesson);
      m.lesson_links.push_back(std::move(out));
    }
    for (const auto& p : in.preferences()) {
      if (p.kind() == v1::Preference::KIND_UNSPECIFIED) continue;
      Preference pref;
      pref.kind = p.kind() == v1::Preference::KIND_PREFER_EARLY
        ? PreferenceKind::kPreferEarly
        : PreferenceKind::kMaxLessonsPerDay;
      pref.year_id = p.year_id();
      pref.division_id = p.division_id();
      pref.subject_id = p.subject_id();
      pref.weight = p.weight();
      pref.param = p.param();
      m.preferences.push_back(pref);
    }
    for (const auto& r : in.daily_load_rules()) {
      m.daily_load_rules.push_back(
        { r.division_id(), r.group_id(), r.min_per_day(), r.max_per_day(),
         r.target_per_day(), r.allowed_deviation(), r.deviation_weight(),
         r.imbalance_weight(), r.overload_weight(), r.underload_weight() });
    }
    return m;
  }

}  // namespace arrango
