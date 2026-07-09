// src/service/convert_report.cc

#include "service/convert.h"
#include "service/convert_common.h"

namespace arrango {
  namespace {

    v1::Conflict::Kind ConflictKindToProto(ConflictKind k) {
      switch (k) {
      case ConflictKind::kTeacherConflict:
        return v1::Conflict::KIND_TEACHER_CONFLICT;
      case ConflictKind::kStudentOverlap:
        return v1::Conflict::KIND_STUDENT_OVERLAP;
      case ConflictKind::kRoomConflict:
        return v1::Conflict::KIND_ROOM_CONFLICT;
      case ConflictKind::kMissingLesson:
        return v1::Conflict::KIND_MISSING_LESSON;
      case ConflictKind::kDuplicateLesson:
        return v1::Conflict::KIND_DUPLICATE_LESSON;
      case ConflictKind::kInvalidRoom:
        return v1::Conflict::KIND_INVALID_ROOM;
      case ConflictKind::kRoomTooSmall:
        return v1::Conflict::KIND_ROOM_TOO_SMALL;
      case ConflictKind::kParallelBlockBroken:
        return v1::Conflict::KIND_PARALLEL_BLOCK_BROKEN;
      case ConflictKind::kLockedMoved:
        return v1::Conflict::KIND_LOCKED_MOVED;
      case ConflictKind::kExternalBlockOverlap:
        return v1::Conflict::KIND_EXTERNAL_BLOCK_OVERLAP;
      case ConflictKind::kOutOfBounds:
        return v1::Conflict::KIND_OUT_OF_BOUNDS;
      case ConflictKind::kInvalidReference:
        return v1::Conflict::KIND_INVALID_REFERENCE;
      case ConflictKind::kInvalidDuration:
        return v1::Conflict::KIND_INVALID_DURATION;
      case ConflictKind::kDailyLoadViolation:
        return v1::Conflict::KIND_DAILY_LOAD_VIOLATION;
      case ConflictKind::kDuplicateDesignator:
        return v1::Conflict::KIND_DUPLICATE_DESIGNATOR;
      case ConflictKind::kTeacherOverloaded:
        return v1::Conflict::KIND_TEACHER_OVERLOADED;
      case ConflictKind::kRoomPoolOverloaded:
        return v1::Conflict::KIND_ROOM_POOL_OVERLOADED;
      case ConflictKind::kLockedCollision:
        return v1::Conflict::KIND_LOCKED_COLLISION;
      case ConflictKind::kAvailabilityHole:
        return v1::Conflict::KIND_AVAILABILITY_HOLE;
      case ConflictKind::kStreamExplosion:
        return v1::Conflict::KIND_STREAM_EXPLOSION;
      case ConflictKind::kGroupSizeMismatch:
        return v1::Conflict::KIND_GROUP_SIZE_MISMATCH;
      case ConflictKind::kInvalidRuleConfig:
        return v1::Conflict::KIND_INVALID_RULE_CONFIG;
      case ConflictKind::kLinkSameDay:
        return v1::Conflict::KIND_LINK_SAME_DAY;
      case ConflictKind::kLinkDifferentDay:
        return v1::Conflict::KIND_LINK_DIFFERENT_DAY;
      case ConflictKind::kLinkConsecutive:
        return v1::Conflict::KIND_LINK_CONSECUTIVE;
      case ConflictKind::kEdgePlacement:
        return v1::Conflict::KIND_EDGE_PLACEMENT;
      }
      return v1::Conflict::KIND_UNSPECIFIED;
    }

    void EntityScoreToProto(const EntityScore& in, v1::EntityScore* out) {
      out->set_entity_id(in.entity_id);
      out->set_name(in.name);
      out->set_quality(in.quality);
      out->set_penalty(in.penalty);
      for (const PenaltyItem& item : in.items) {
        v1::PenaltyItem* pi = out->add_items();
        pi->set_category(item.category);
        pi->set_penalty(item.penalty);
        pi->set_count(item.count);
      }
    }

    v1::EntityKind EntityKindToProto(EntityKind k) {
      switch (k) {
      case EntityKind::kDivision: return v1::ENTITY_KIND_DIVISION;
      case EntityKind::kGroup: return v1::ENTITY_KIND_GROUP;
      case EntityKind::kTeacher: return v1::ENTITY_KIND_TEACHER;
      case EntityKind::kRoom: return v1::ENTITY_KIND_ROOM;
      case EntityKind::kSubject: return v1::ENTITY_KIND_SUBJECT;
      case EntityKind::kYear: return v1::ENTITY_KIND_YEAR;
      case EntityKind::kExternalBlock: return v1::ENTITY_KIND_EXTERNAL_BLOCK;
      case EntityKind::kUnspecified: break;
      }
      return v1::ENTITY_KIND_UNSPECIFIED;
    }

    // Fills the structured-locus repeated fields; works for any message that has
    // entities / lessons / spans (Conflict and SoftIssue share the field names).
    template <class Msg>
    void LocusToProto(const IssueLocus& in, Msg* out) {
      for (const EntityRef& e : in.entities) {
        v1::EntityRef* pe = out->add_entities();
        pe->set_kind(EntityKindToProto(e.kind));
        pe->set_id(e.id);
      }
      for (const LessonRef& l : in.lessons) {
        v1::LessonRef* pl = out->add_lessons();
        pl->set_lesson_id(l.lesson_id);
        pl->set_day_id(l.day_id == kNoId ? 0 : l.day_id);
        pl->set_start_period(l.start_period);
        pl->set_duration(l.duration);
        pl->set_room_id(l.room_id == kNoId ? 0 : l.room_id);
      }
      for (const TimeSpan& s : in.spans) {
        v1::TimeSpan* ps = out->add_spans();
        ps->set_day_id(s.day_id == kNoId ? 0 : s.day_id);
        ps->set_start_period(s.start_period);
        ps->set_period_span(s.period_span);
      }
    }

  }  // namespace

  ScheduleSnapshot SnapshotFromProto(const v1::ScheduleSnapshot& in) {
    ScheduleSnapshot s;
    for (const auto& sl : in.lessons()) {
      s.lessons.push_back({ sl.lesson_id(), PlacementFromProto(sl.placement()) });
    }
    return s;
  }

  void SnapshotToProto(const ScheduleSnapshot& in, v1::ScheduleSnapshot* out) {
    for (const auto& sl : in.lessons) {
      v1::ScheduledLesson* psl = out->add_lessons();
      psl->set_lesson_id(sl.lesson_id);
      PlacementToProto(sl.placement, psl->mutable_placement());
    }
  }

  void ValidationToProto(const ValidationReport& in, v1::ValidationReport* out) {
    out->set_valid(in.valid);
    for (const Conflict& c : in.conflicts) {
      v1::Conflict* pc = out->add_conflicts();
      pc->set_kind(ConflictKindToProto(c.kind));
      pc->set_message(c.message);
      for (Id id : c.lesson_ids) pc->add_lesson_ids(id);
      pc->set_entity_id(c.entity_id);
      pc->set_day_id(c.day_id);
      pc->set_period(c.period);
      LocusToProto(c.locus, pc);
    }
  }

  void ScoreToProto(const ScoreReport& in, v1::ScoreReport* out) {
    out->set_overall_quality(in.overall_quality);
    out->set_all_students_quality(in.all_students_quality);
    out->set_all_teachers_quality(in.all_teachers_quality);
    out->set_total_penalty(in.total_penalty);
    for (const auto& e : in.division_scores) {
      EntityScoreToProto(e, out->add_division_scores());
    }
    for (const auto& e : in.year_scores) {
      EntityScoreToProto(e, out->add_year_scores());
    }
    for (const auto& e : in.teacher_scores) {
      EntityScoreToProto(e, out->add_teacher_scores());
    }
    for (const PenaltyItem& item : in.global_items) {
      v1::PenaltyItem* pi = out->add_global_items();
      pi->set_category(item.category);
      pi->set_penalty(item.penalty);
      pi->set_count(item.count);
    }
    auto issue_to_proto = [] (const SoftIssue& issue, v1::SoftIssue* pi) {
      pi->set_category(issue.category);
      pi->set_entity(issue.entity);
      pi->set_entity_id(issue.entity_id);
      pi->set_teacher(issue.teacher);
      pi->set_day_id(issue.day_id);
      pi->set_period(issue.period);
      pi->set_count(issue.count);
      pi->set_penalty(issue.penalty);
      pi->set_config_hard(issue.config_hard);
      LocusToProto(issue.locus, pi);
      };
    for (const SoftIssue& issue : in.soft_issues) {
      issue_to_proto(issue, out->add_soft_issues());
    }
    for (const SoftIssue& issue : in.info_issues) {
      issue_to_proto(issue, out->add_info_issues());
    }
    for (const MetricScore& metric : in.metric_scores) {
      v1::MetricScore* pm = out->add_metric_scores();
      pm->set_key(metric.key);
      pm->set_teachers(metric.teachers);
      pm->set_applicable(metric.applicable);
      pm->set_rate(metric.rate);
      pm->set_subscore(metric.subscore);
      pm->set_count(metric.count);
    }
  }

  void CoreToProto(const InfeasibleCore& in, v1::InfeasibleCore* out) {
    auto fill = [] (const CoreItem& item, v1::InfeasibleCoreItem* pi) {
      pi->set_kind(item.kind);
      pi->set_rule(item.rule);
      pi->set_entity_id(item.entity_id);
      pi->set_entity_name(item.entity_name);
      for (Id lesson : item.lesson_ids) pi->add_lesson_ids(lesson);
      pi->set_message(item.message);
      };
    for (const CoreItem& item : in.items) fill(item, out->add_items());
    for (const CoreItem& item : in.hints) fill(item, out->add_hints());
    out->set_minimal(in.minimal);
    out->set_message(in.message);
  }

  void ProgressToProto(const SolveProgressInfo& in, v1::SolveProgress* out) {
    switch (in.stage) {
    case SolveStage::kPreflight:
      out->set_stage(v1::SOLVE_STAGE_PREFLIGHT); break;
    case SolveStage::kCandidates:
      out->set_stage(v1::SOLVE_STAGE_CANDIDATES); break;
    case SolveStage::kConstruct:
      out->set_stage(v1::SOLVE_STAGE_CONSTRUCT); break;
    case SolveStage::kDirect:
      out->set_stage(v1::SOLVE_STAGE_DIRECT); break;
    case SolveStage::kLns:
      out->set_stage(v1::SOLVE_STAGE_LNS); break;
    case SolveStage::kValidate:
      out->set_stage(v1::SOLVE_STAGE_VALIDATE); break;
    case SolveStage::kDone:
      out->set_stage(v1::SOLVE_STAGE_DONE); break;
    case SolveStage::kExplain:
      out->set_stage(v1::SOLVE_STAGE_EXPLAIN); break;
    case SolveStage::kUnspecified:
      out->set_stage(v1::SOLVE_STAGE_UNSPECIFIED); break;
    }
    out->set_detail(in.detail);
    out->set_candidates(in.candidates);
    out->set_lns_pass(in.lns_pass);
    out->set_lns_neighborhood(in.lns_neighborhood);
    out->set_lns_neighborhoods_total(in.lns_neighborhoods_total);
    out->set_lns_accepted(in.lns_accepted);
    out->set_lns_rejected_worse(in.lns_rejected_worse);
    out->set_lns_rejected_invalid(in.lns_rejected_invalid);
    out->set_stage_elapsed_seconds(in.stage_elapsed_seconds);
  }

  void VerdictToProto(const ScheduleVerdict& in, v1::ScheduleVerdict* out) {
    out->set_tier(in.tier == VerdictTier::kErrors
      ? v1::ScheduleVerdict::TIER_ERRORS
      : in.tier == VerdictTier::kWarnings
      ? v1::ScheduleVerdict::TIER_WARNINGS
      : v1::ScheduleVerdict::TIER_PRISTINE);
    out->set_errors(in.errors);
    out->set_warnings(in.warnings);
    out->set_infos(in.infos);
    out->set_config_hard_errors(in.config_hard_errors);
  }

}  // namespace arrango
