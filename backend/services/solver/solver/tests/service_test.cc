// tests/service_test.cc

#include <gtest/gtest.h>

#include <chrono>
#include <map>
#include <memory>
#include <thread>

#include <google/protobuf/util/message_differencer.h>
#include <grpcpp/grpcpp.h>

#include "service/convert.h"
#include "service/service.h"
#include "test_school.h"

namespace arrango {
  namespace {

    class ServiceTest : public ::testing::Test {
    protected:
      void SetUp() override {
        grpc::ServerBuilder builder;
        builder.AddListeningPort("127.0.0.1:0",
          grpc::InsecureServerCredentials(), &port_);
        builder.RegisterService(&service_);
        server_ = builder.BuildAndStart();
        ASSERT_NE(server_, nullptr);
        ASSERT_GT(port_, 0);
        stub_ = v1::SolverService::NewStub(grpc::CreateChannel(
          "127.0.0.1:" + std::to_string(port_),
          grpc::InsecureChannelCredentials()));
      }

      void TearDown() override { server_->Shutdown(); }

      v1::SchoolModel DemoModel(v1::DemoPreset preset) {
        grpc::ClientContext ctx;
        v1::DemoRequest req;
        req.set_preset(preset);
        req.set_seed(1);
        v1::SchoolModel model;
        EXPECT_TRUE(stub_->GetDemoSchool(&ctx, req, &model).ok());
        return model;
      }

      // The small fixture the deleted tiny preset used to provide, as proto.
      v1::SchoolModel TestModel() {
        v1::SchoolModel model;
        ModelToProto(TestSchool(), &model);
        return model;
      }

      SolverServiceImpl service_;
      int port_ = 0;
      std::unique_ptr<grpc::Server> server_;
      std::unique_ptr<v1::SolverService::Stub> stub_;
    };

    TEST_F(ServiceTest, GetDemoSchoolReturnsModel) {
      v1::SchoolModel production = DemoModel(v1::DEMO_PRESET_PRODUCTION);
      EXPECT_EQ(production.divisions_size(), 34);
      EXPECT_GT(production.lessons_size(), 1000);
      v1::SchoolModel mega = DemoModel(v1::DEMO_PRESET_MEGA);
      EXPECT_GT(mega.divisions_size(), production.divisions_size());
      EXPECT_GT(mega.lessons_size(), production.lessons_size());
      EXPECT_GT(mega.lesson_links_size(), 0);
    }

    TEST_F(ServiceTest, ModelProtoRoundTripPreservesNewFields) {
      v1::SchoolModel model = TestModel();
      model.mutable_rooms(0)->set_capacity(30);
      model.mutable_rooms(0)->set_capacity_source(v1::COUNT_SOURCE_MANUAL);
      model.mutable_rooms(0)->set_source_ref("s1");
      model.mutable_groups(0)->set_student_count(14);
      model.mutable_groups(0)->set_count_source(v1::COUNT_SOURCE_IMPORTED);
      model.mutable_divisions(0)->set_student_count(28);
      model.mutable_divisions(0)->set_count_source(v1::COUNT_SOURCE_IMPORTED);
      auto* lesson = model.mutable_lessons(0);
      lesson->add_disallowed_room_designators("SG1");
      lesson->add_observed_room_designators("s101");
      auto* rule = model.add_daily_load_rules();
      rule->set_min_per_day(3);
      rule->set_target_per_day(6);
      rule->set_underload_weight(200);
      auto* split = model.add_splits();
      split->set_id(9001);
      split->set_name("pe");
      split->set_division_id(model.divisions(0).id());
      split->set_kind(v1::SPLIT_KIND_FIXED);
      model.mutable_groups(0)->set_split_id(9001);
      auto* rules = model.mutable_rule_config();
      rules->set_profile("dobry_plan");
      auto* override1 = rules->add_overrides();
      override1->set_rule("subject_once");
      override1->set_mode(v1::RULE_MODE_OFF);
      override1->set_subject_id(model.subjects(0).id());
      auto* override2 = rules->add_overrides();
      override2->set_rule("teacher_gaps");
      override2->set_mode(v1::RULE_MODE_SOFT);
      override2->set_weight(180);
      override2->set_teacher_id(model.teachers(0).id());
      model.mutable_weights()->set_single_lesson_day_base(25);

      v1::SchoolModel back;
      ModelToProto(ModelFromProto(model), &back);
      EXPECT_TRUE(
        google::protobuf::util::MessageDifferencer::Equals(model, back));
    }

    // ReportingWeights reshape the RATING only: a huge gaps half-life makes the
    // same gapped schedule score higher, and the raw penalty stays identical.
    TEST_F(ServiceTest, ReportingWeightsChangeQualityNotPenalty) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"} };
      for (Id id : {Id{ 100 }, Id{ 101 }}) {
        m.lessons.push_back({ .id = id, .participants = {{20, kNoId}},
                             .subject_id = 50, .teacher_id = 40,
                             .requires_room = false });
      }
      ScheduleSnapshot gapped{ {{100, {1, 0, kNoId}}, {101, {1, 2, kNoId}}} };

      v1::ScoreRequest req;
      ModelToProto(m, req.mutable_model());
      SnapshotToProto(gapped, req.mutable_snapshot());

      v1::ScoreReport def, soft;
      {
        grpc::ClientContext ctx;
        ASSERT_TRUE(stub_->Score(&ctx, req, &def).ok());
      }
      (*req.mutable_reporting()->mutable_half_life())["gaps"] = 1000.0;
      {
        grpc::ClientContext ctx;
        ASSERT_TRUE(stub_->Score(&ctx, req, &soft).ok());
      }
      EXPECT_GT(soft.all_students_quality(), def.all_students_quality());
      EXPECT_EQ(soft.total_penalty(), def.total_penalty());
    }

    // The audit is the last line of defense: an honest result passes, a wrong
    // objective or an invalid snapshot is named.
    TEST_F(ServiceTest, AuditCatchesCorruptedResults) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"} };
      for (Id id : {Id{ 100 }, Id{ 101 }}) {
        m.lessons.push_back({ .id = id, .participants = {{20, kNoId}},
                             .subject_id = 50, .teacher_id = 40,
                             .requires_room = false });
      }
      SolveResult honest;
      honest.best = ScheduleSnapshot{ {{100, {1, 0, kNoId}}, {101, {1, 1, kNoId}}} };
      honest.objective = ComputeScore(m, honest.best).total_penalty;
      EXPECT_TRUE(AuditResult(m, {}, honest).ok);

      SolveResult lying = honest;
      lying.objective += 1;
      AuditVerdict wrong_objective = AuditResult(m, {}, lying);
      EXPECT_FALSE(wrong_objective.ok);
      EXPECT_NE(wrong_objective.reason.find("objective"), std::string::npos);

      SolveResult invalid = honest;
      invalid.best.lessons[1].placement.start_period = 0;  // double-booked
      invalid.objective = ComputeScore(m, invalid.best).total_penalty;
      AuditVerdict bad = AuditResult(m, {}, invalid);
      EXPECT_FALSE(bad.ok);
      EXPECT_NE(bad.reason.find("validation"), std::string::npos);
    }

    // The never-regress baseline must satisfy every HARD rule: a previous
    // timetable with a subject double is rejected under subject_once=hard and
    // accepted under the default soft mode.
    TEST_F(ServiceTest, HardRuleViolatingBaselineIsRejected) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      // Contiguous previous timetable, but Mat runs at periods 0 AND 2.
      for (auto [id, subject, period] :
        { std::tuple<Id, Id, uint32_t>{100, 50, 0}, {101, 51, 1}, {102, 50, 2} }) {
        LessonInstance l{ .id = id, .participants = {{20, kNoId}},
                         .subject_id = subject, .teacher_id = 40,
                         .requires_room = false };
        l.previous_placement = Placement{ 1, period, kNoId };
        m.lessons.push_back(l);
      }

      auto baseline_streamed = [&] (const SchoolModel& variant) {
        v1::SolveRequest req;
        ModelToProto(variant, req.mutable_model());
        req.mutable_params()->set_max_time_seconds(8);
        req.mutable_params()->set_num_workers(1);
        req.mutable_params()->set_random_seed(7);
        grpc::ClientContext ctx;
        auto reader = stub_->Solve(&ctx, req);
        bool baseline = false;
        v1::SolveUpdate update;
        while (reader->Read(&update)) {
          baseline |= update.message() == "baseline: previous timetable";
        }
        EXPECT_TRUE(reader->Finish().ok());
        return baseline;
        };

      EXPECT_TRUE(baseline_streamed(m));  // default: doubles are merely soft
      SchoolModel strict = m;
      strict.rule_config.overrides = { {"subject_once", RuleMode::kHard} };
      EXPECT_FALSE(baseline_streamed(strict));

      // The same strictness sent at REQUEST level (model untouched) must reach
      // the scoring/baseline path identically — and be echoed in the config so
      // stored results record the philosophy that produced them.
      v1::SolveRequest req;
      ModelToProto(m, req.mutable_model());
      req.mutable_params()->set_max_time_seconds(8);
      req.mutable_params()->set_num_workers(1);
      auto* o = req.mutable_rule_config()->add_overrides();
      o->set_rule("subject_once");
      o->set_mode(v1::RULE_MODE_HARD);
      grpc::ClientContext ctx;
      auto reader = stub_->Solve(&ctx, req);
      bool baseline = false;
      bool echoed = false;
      v1::SolveUpdate update;
      while (reader->Read(&update)) {
        baseline |= update.message() == "baseline: previous timetable";
        for (const auto& eo : update.config().rule_config().overrides()) {
          echoed |= eo.rule() == "subject_once" &&
            eo.mode() == v1::RULE_MODE_HARD;
        }
      }
      EXPECT_TRUE(reader->Finish().ok());
      EXPECT_FALSE(baseline);
      EXPECT_TRUE(echoed);
    }

    // An infeasible solve must not end at "proven infeasible": the stream
    // shows the EXPLAIN stage and DONE carries a core naming the clashing
    // inputs. Disabling the explainer suppresses both.
    TEST_F(ServiceTest, InfeasibleSolveStreamsExplanation) {
      SchoolModel m;
      m.days = { {1, "Pon", 6} };
      for (Id p = 1; p <= 6; ++p) m.periods.push_back({ 200 + p, "" });
      m.years = { {10, "R1", 1, 100} };
      m.divisions = { {20, "1A", 10} };
      m.teachers = { {40, "TA"}, {41, "TB"} };
      m.subjects = { {50, "Mat"}, {51, "Ang"} };
      // Locks at p0 and p3 leave a 2-period hole; gap_windows is hard.
      for (auto [id, subject, teacher, period] :
        { std::tuple<Id, Id, Id, uint32_t>{100, 50, 40, 0},
         {101, 51, 41, 3} }) {
        LessonInstance l{ .id = id, .participants = {{20, kNoId}},
                         .subject_id = subject, .teacher_id = teacher,
                         .requires_room = false };
        l.locked = true;
        l.locked_placement = Placement{ 1, period, kNoId };
        m.lessons.push_back(l);
      }

      auto run = [&] (bool disable) {
        v1::SolveRequest req;
        ModelToProto(m, req.mutable_model());
        auto* config = req.mutable_config();
        config->set_total_time_seconds(20);
        config->set_num_workers(1);
        config->set_disable_infeasibility_explainer(disable);
        grpc::ClientContext ctx;
        auto reader = stub_->Solve(&ctx, req);
        bool explain_stage = false;
        v1::SolveUpdate done;
        v1::SolveUpdate update;
        while (reader->Read(&update)) {
          explain_stage |= update.has_progress() &&
            update.progress().stage() == v1::SOLVE_STAGE_EXPLAIN;
          if (update.phase() == v1::SOLVE_PHASE_DONE) done = update;
        }
        EXPECT_TRUE(reader->Finish().ok());
        EXPECT_EQ(done.status(), v1::SOLVE_STATUS_INFEASIBLE);
        return std::make_pair(explain_stage, done);
        };

      auto [stage_on, done_on] = run(false);
      EXPECT_TRUE(stage_on);
      ASSERT_TRUE(done_on.has_infeasible_core());
      bool lock = false, rule = false;
      for (const auto& item : done_on.infeasible_core().items()) {
        lock |= item.kind() == "locked_lesson";
        rule |= item.kind() == "hard_rule" && item.rule() == "gap_windows";
      }
      EXPECT_TRUE(lock);
      EXPECT_TRUE(rule);
      EXPECT_NE(done_on.message().find("cannot satisfy together"),
        std::string::npos);

      auto [stage_off, done_off] = run(true);
      EXPECT_FALSE(stage_off);
      EXPECT_FALSE(done_off.has_infeasible_core());
    }

    TEST_F(ServiceTest, ValidateFlagsEmptySnapshot) {
      v1::SchoolModel model = TestModel();
      grpc::ClientContext ctx;
      v1::ValidateRequest req;
      *req.mutable_model() = model;
      v1::ValidationReport report;
      ASSERT_TRUE(stub_->Validate(&ctx, req, &report).ok());
      EXPECT_FALSE(report.valid());
      bool missing = false;
      for (const auto& c : report.conflicts()) {
        missing |= c.kind() == v1::Conflict::KIND_MISSING_LESSON;
      }
      EXPECT_TRUE(missing);
    }

    TEST_F(ServiceTest, ScoreReturnsBreakdowns) {
      v1::SchoolModel model = TestModel();
      grpc::ClientContext ctx;
      v1::ScoreRequest req;
      *req.mutable_model() = model;
      v1::ScoreReport report;
      ASSERT_TRUE(stub_->Score(&ctx, req, &report).ok());
      EXPECT_EQ(report.division_scores_size(), 2);
      EXPECT_GT(report.overall_quality(), 0.0);
    }

    // The stream must narrate what the solver is doing: stage events arrive in
    // pipeline order and the verdict rides on every solution update.
    TEST_F(ServiceTest, SolveStreamsStageProgressAndVerdict) {
      v1::SchoolModel model = TestModel();
      grpc::ClientContext ctx;
      v1::SolveRequest req;
      *req.mutable_model() = model;
      req.mutable_params()->set_max_time_seconds(10);
      req.mutable_params()->set_num_workers(1);
      req.mutable_params()->set_random_seed(7);
      auto reader = stub_->Solve(&ctx, req);

      std::vector<v1::SolveStage> stages;
      bool solution_has_verdict = false;
      v1::SolveUpdate update;
      while (reader->Read(&update)) {
        if (update.has_progress() &&
          (stages.empty() || stages.back() != update.progress().stage())) {
          stages.push_back(update.progress().stage());
        }
        if (update.phase() == v1::SOLVE_PHASE_SOLUTION) {
          solution_has_verdict |=
            update.verdict().tier() != v1::ScheduleVerdict::TIER_UNSPECIFIED;
        }
      }
      ASSERT_TRUE(reader->Finish().ok());

      auto index_of = [&] (v1::SolveStage s) {
        for (size_t i = 0; i < stages.size(); ++i) {
          if (stages[i] == s) return static_cast<int>(i);
        }
        return -1;
        };
      const int preflight = index_of(v1::SOLVE_STAGE_PREFLIGHT);
      const int candidates = index_of(v1::SOLVE_STAGE_CANDIDATES);
      const int direct = index_of(v1::SOLVE_STAGE_DIRECT);
      const int validate = index_of(v1::SOLVE_STAGE_VALIDATE);
      ASSERT_GE(preflight, 0);
      ASSERT_GE(candidates, 0);
      ASSERT_GE(direct, 0);
      ASSERT_GE(validate, 0);
      EXPECT_LT(preflight, candidates);
      EXPECT_LT(candidates, direct);
      EXPECT_LT(direct, validate);
      EXPECT_TRUE(solution_has_verdict);
    }

    TEST_F(ServiceTest, SolveStreamsStartedSolutionsDone) {
      v1::SchoolModel model = TestModel();
      grpc::ClientContext ctx;
      v1::SolveRequest req;
      *req.mutable_model() = model;
      req.mutable_params()->set_max_time_seconds(10);
      req.mutable_params()->set_num_workers(1);
      req.mutable_params()->set_random_seed(7);
      auto reader = stub_->Solve(&ctx, req);

      std::vector<v1::SolveUpdate> updates;
      v1::SolveUpdate update;
      while (reader->Read(&update)) updates.push_back(update);
      ASSERT_TRUE(reader->Finish().ok());

      ASSERT_GE(updates.size(), 3u);  // STARTED + >=1 SOLUTION + DONE
      EXPECT_EQ(updates.front().phase(), v1::SOLVE_PHASE_STARTED);
      EXPECT_EQ(updates.back().phase(), v1::SOLVE_PHASE_DONE);
      bool any_solution = false;
      for (const auto& u : updates) {
        any_solution |= u.phase() == v1::SOLVE_PHASE_SOLUTION;
      }
      EXPECT_TRUE(any_solution);

      const v1::SolveUpdate& done = updates.back();
      EXPECT_TRUE(done.status() == v1::SOLVE_STATUS_OPTIMAL ||
        done.status() == v1::SOLVE_STATUS_FEASIBLE);
      EXPECT_TRUE(done.validation().valid());
      EXPECT_EQ(done.snapshot().lessons_size(), model.lessons_size());
      EXPECT_GT(done.score().overall_quality(), 0.0);
    }

    // A complete, valid set of previous placements is the baseline: it is
    // streamed as the first solution and the final result never scores worse.
    TEST_F(ServiceTest, PreviousTimetableIsBaselineAndNeverRegressed) {
      v1::SchoolModel model = TestModel();
      // First solve to get a valid schedule.
      std::map<uint32_t, v1::Placement> placed;
      {
        grpc::ClientContext ctx;
        v1::SolveRequest req;
        *req.mutable_model() = model;
        req.mutable_params()->set_max_time_seconds(10);
        req.mutable_params()->set_num_workers(1);
        req.mutable_params()->set_random_seed(7);
        auto reader = stub_->Solve(&ctx, req);
        v1::SolveUpdate update;
        v1::SolveUpdate last;
        while (reader->Read(&update)) last = update;
        ASSERT_TRUE(reader->Finish().ok());
        ASSERT_TRUE(last.validation().valid());
        for (const auto& sl : last.snapshot().lessons()) {
          placed[sl.lesson_id()] = sl.placement();
        }
      }
      for (auto& lesson : *model.mutable_lessons()) {
        lesson.set_has_previous(true);
        *lesson.mutable_previous_placement() = placed.at(lesson.id());
      }
      // Re-solve with a limit too short to find anything from scratch.
      grpc::ClientContext ctx;
      v1::SolveRequest req;
      *req.mutable_model() = model;
      req.mutable_params()->set_max_time_seconds(0.01);
      req.mutable_params()->set_num_workers(1);
      req.mutable_params()->set_random_seed(7);
      auto reader = stub_->Solve(&ctx, req);
      std::vector<v1::SolveUpdate> updates;
      v1::SolveUpdate update;
      while (reader->Read(&update)) updates.push_back(update);
      ASSERT_TRUE(reader->Finish().ok());

      // Baseline streamed right after STARTED.
      ASSERT_GE(updates.size(), 3u);
      EXPECT_EQ(updates[1].phase(), v1::SOLVE_PHASE_SOLUTION);
      EXPECT_EQ(updates[1].message(), "baseline: previous timetable");

      const v1::SolveUpdate& done = updates.back();
      EXPECT_TRUE(done.status() == v1::SOLVE_STATUS_OPTIMAL ||
        done.status() == v1::SOLVE_STATUS_FEASIBLE);
      EXPECT_TRUE(done.validation().valid());
      for (const auto& sl : done.snapshot().lessons()) {
        const v1::Placement& want = placed.at(sl.lesson_id());
        EXPECT_EQ(sl.placement().day_id(), want.day_id());
        EXPECT_EQ(sl.placement().start_period(), want.start_period());
      }
    }

    TEST_F(ServiceTest, FromScratchSkipsBaselineAndHints) {
      v1::SchoolModel model = TestModel();
      for (auto& lesson : *model.mutable_lessons()) {
        lesson.set_has_previous(true);
        *lesson.mutable_previous_placement() = lesson.locked()
          ? lesson.locked_placement()
          : v1::Placement{};
      }
      grpc::ClientContext ctx;
      v1::SolveRequest req;
      *req.mutable_model() = model;
      req.mutable_params()->set_max_time_seconds(10);
      req.mutable_params()->set_num_workers(1);
      req.mutable_params()->set_random_seed(7);
      req.mutable_params()->set_from_scratch(true);
      auto reader = stub_->Solve(&ctx, req);
      std::vector<v1::SolveUpdate> updates;
      v1::SolveUpdate update;
      while (reader->Read(&update)) updates.push_back(update);
      ASSERT_TRUE(reader->Finish().ok());
      for (const auto& u : updates) {
        EXPECT_NE(u.message(), "baseline: previous timetable");
      }
      EXPECT_TRUE(updates.back().validation().valid());
    }

    TEST_F(ServiceTest, ConfigEchoedWithCopiedWeights) {
      v1::SchoolModel model = TestModel();
      grpc::ClientContext ctx;
      v1::SolveRequest req;
      *req.mutable_model() = model;
      auto* cfg = req.mutable_config();
      cfg->set_mode(v1::SOLVE_MODE_FULL_PIPELINE);
      cfg->set_total_time_seconds(5);
      cfg->set_random_seed(7);
      auto reader = stub_->Solve(&ctx, req);

      v1::SolveUpdate update;
      bool saw_any = false;
      while (reader->Read(&update)) {
        saw_any = true;
        // Every update echoes the resolved config.
        EXPECT_EQ(update.config().total_time_seconds(), 5);
        EXPECT_EQ(update.config().random_seed(), 7);
        // Weights unset in the request are filled from the model.
        EXPECT_NE(update.config().weights().student_gap_base(), 0);
      }
      ASSERT_TRUE(saw_any);
      ASSERT_TRUE(reader->Finish().ok());
      EXPECT_TRUE(update.status() == v1::SOLVE_STATUS_OPTIMAL ||
        update.status() == v1::SOLVE_STATUS_FEASIBLE);
    }

    TEST_F(ServiceTest, UnimplementedModeReturnsError) {
      v1::SchoolModel model = TestModel();
      grpc::ClientContext ctx;
      v1::SolveRequest req;
      *req.mutable_model() = model;
      req.mutable_config()->set_mode(v1::SOLVE_MODE_REPAIR);
      auto reader = stub_->Solve(&ctx, req);

      std::vector<v1::SolveUpdate> updates;
      v1::SolveUpdate update;
      while (reader->Read(&update)) updates.push_back(update);
      ASSERT_TRUE(reader->Finish().ok());
      ASSERT_EQ(updates.size(), 1u);
      EXPECT_EQ(updates[0].phase(), v1::SOLVE_PHASE_DONE);
      EXPECT_EQ(updates[0].status(), v1::SOLVE_STATUS_ERROR);
      EXPECT_NE(updates[0].message().find("not implemented"), std::string::npos);
    }

    TEST_F(ServiceTest, ClientCancelStopsSolvePromptly) {
      v1::SchoolModel model = DemoModel(v1::DEMO_PRESET_PRODUCTION);
      grpc::ClientContext ctx;
      v1::SolveRequest req;
      *req.mutable_model() = model;
      req.mutable_params()->set_max_time_seconds(120);
      req.mutable_params()->set_num_workers(1);
      req.mutable_params()->set_random_seed(7);
      auto reader = stub_->Solve(&ctx, req);

      v1::SolveUpdate update;
      ASSERT_TRUE(reader->Read(&update));  // STARTED
      const auto cancel_at = std::chrono::steady_clock::now();
      ctx.TryCancel();
      while (reader->Read(&update)) {
      }
      reader->Finish();  // status CANCELLED is fine
      const double waited =
        std::chrono::duration<double>(std::chrono::steady_clock::now() -
          cancel_at)
        .count();
      EXPECT_LT(waited, 10.0);
    }

  }  // namespace
}  // namespace arrango
