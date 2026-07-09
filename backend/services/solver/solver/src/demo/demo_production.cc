// src/demo/demo_production.cc

#include "demo/demo_internal.h"

namespace arrango {
  namespace demo_detail {

    // The shared technikum generator: production-scale raw data modelled on the
    // real normalised Optivum export. Emits ONLY the problem: entities + lessons
    // to place. `mega` scales everything up ~35% AND layers every solver
    // capability on top (splits, locks, links, edges, rules, load bands) so one
    // preset exercises the whole feature surface.
    namespace {

      struct TechnikumScale {
        const char* name;
        int sala, prac, gim, jez;
        double pool_scale;
        bool mega_features;
      };

      SchoolModel BuildTechnikum(Builder& b, const TechnikumScale& scale) {
        b.m.name = scale.name;
        b.Calendar(5, 14);  // real technikum: 5 days, up to 14 periods
        auto scaled = [&] (int n) {
          return std::max(n, static_cast<int>(n * scale.pool_scale));
          };
        std::array<Id, 5> years = {
            b.AddYear(1, 300), b.AddYear(2, 150), b.AddYear(3, 100),
            b.AddYear(4, 150), b.AddYear(5, 300) };

        // Rooms in the real Optivum categories, each with its own designator family.
        auto make_rooms = [&] (const std::string& prefix, int n) {
          std::vector<std::string> out;
          for (int i = 1; i <= n; ++i) {
            std::string name = prefix + std::to_string(i);
            b.AddRoom(name);
            out.push_back(name);
          }
          return out;
          };
        const std::vector<std::string> sala = make_rooms("s", scale.sala);
        const std::vector<std::string> prac = make_rooms("PR", scale.prac);
        const std::vector<std::string> gim = make_rooms("sg", scale.gim);
        const std::vector<std::string> jez = make_rooms("sj", scale.jez);
        b.AddRoom("KAT");                        // religia
        b.AddRoom("SW");                         // świetlica / reserve
        const std::vector<std::string> kat = { "KAT" };
        b.sala_pool = sala;  // teachers draw home rooms from the classroom pool

        // Subjects, grouped by the room family their lessons need.
        Id mat = b.AddSubject("matematyka");
        Id pol = b.AddSubject("j.polski");
        Id ang = b.AddSubject("j.ang");
        Id niem = b.AddSubject("j.niemiecki");
        Id hist = b.AddSubject("historia");
        Id wos = b.AddSubject("WOS");
        Id fiz = b.AddSubject("fizyka");
        Id chem = b.AddSubject("chemia");
        Id biol = b.AddSubject("biologia");
        Id geo = b.AddSubject("geografia");
        Id wf = b.AddSubject("wf");
        Id rel = b.AddSubject("religia");
        Id wych = b.AddSubject("godz.wych");
        Id inf = b.AddSubject("informatyka");
        Id siec = b.AddSubject("sieci.komp");
        Id prog = b.AddSubject("prog.apk.mob", /*prefers_blocks=*/true);
        Id sysm = b.AddSubject("sys.mikropr");
        Id adm = b.AddSubject("adm.sys.op", /*prefers_blocks=*/true);
        Id elek = b.AddSubject("elektrotechn");
        Id bazy = b.AddSubject("prac.baz.dan", /*prefers_blocks=*/true);

        // Teacher pools per subject area (sum ≈ 86 teachers).
        auto mat_pool = MakePool(b, "mat", scaled(8));
        auto pol_pool = MakePool(b, "pol", scaled(6));
        auto ang_pool = MakePool(b, "ang", scaled(8));
        auto niem_pool = MakePool(b, "niem", scaled(3));
        auto hist_pool = MakePool(b, "hist", scaled(4));   // historia + WOS
        auto przyr_pool = MakePool(b, "przyr", scaled(7));  // fizyka/chemia/biologia/geografia
        auto wf_pool = MakePool(b, "wf", scaled(6));
        auto rel_pool = MakePool(b, "rel", scaled(2));
        auto inf_pool = MakePool(b, "inf", scaled(12));
        auto siec_pool = MakePool(b, "siec", scaled(6));
        auto prog_pool = MakePool(b, "prog", scaled(7));
        auto sysm_pool = MakePool(b, "sysm", scaled(4));
        auto adm_pool = MakePool(b, "adm", scaled(4));
        auto elek_pool = MakePool(b, "elek", scaled(5));
        auto bazy_pool = MakePool(b, "bazy", scaled(4));

        // Lesson plan for one division: base subjects everyone takes, general
        // sciences in the lower years, vocational IT labs (deepening) in the upper
        // years. Split subjects (languages, PE, labs) run their groups in parallel.
        auto plans_for = [&] (int level, int groups) {
          const int lang_ways = std::min(groups, 2);  // languages split 2 ways
          const int voc_ways = std::min(groups, 3);   // labs split up to 3 ways
          std::vector<SubjectPlan> p = {
              {.subject = mat, .count = 4, .room_designators = sala,
               .home_rooms = true, .teacher_pool = &mat_pool},
              {.subject = pol, .count = 3, .room_designators = sala,
               .home_rooms = true, .teacher_pool = &pol_pool},
              {.subject = hist, .count = 2, .room_designators = sala,
               .home_rooms = true, .teacher_pool = &hist_pool},
              {.subject = rel, .count = 1, .room_designators = kat,
               .teacher_pool = &rel_pool},
              {.subject = wych, .count = 1, .room_designators = sala,
               .requires_teacher = false, .home_rooms = true},
              {.subject = ang, .count = 3, .split_ways = lang_ways, .parallel = true,
               .room_designators = jez, .teacher_pool = &ang_pool},
              {.subject = wf, .count = 3, .split_ways = lang_ways, .parallel = true,
               .room_designators = gim, .teacher_pool = &wf_pool},
          };
          if (level <= 2) {
            // Lower years: broad general education.
            p.push_back({ .subject = fiz, .count = 2, .room_designators = sala,
                         .home_rooms = true, .teacher_pool = &przyr_pool });
            p.push_back({ .subject = chem, .count = 1, .room_designators = sala,
                         .home_rooms = true, .teacher_pool = &przyr_pool });
            p.push_back({ .subject = biol, .count = 1, .room_designators = sala,
                         .home_rooms = true, .teacher_pool = &przyr_pool });
            p.push_back({ .subject = geo, .count = 1, .room_designators = sala,
                         .home_rooms = true, .teacher_pool = &przyr_pool });
            p.push_back({ .subject = wos, .count = 1, .room_designators = sala,
                         .home_rooms = true, .teacher_pool = &hist_pool });
            p.push_back({ .subject = niem, .count = 2, .split_ways = lang_ways,
                         .parallel = true, .room_designators = jez,
                         .teacher_pool = &niem_pool });
          }
          else {
            // Upper years: less general, more vocational lab time.
            p.push_back({ .subject = fiz, .count = 1, .room_designators = sala,
                         .home_rooms = true, .teacher_pool = &przyr_pool });
            p.push_back({ .subject = wos, .count = 1, .room_designators = sala,
                         .home_rooms = true, .teacher_pool = &hist_pool });
            p.push_back({ .subject = inf, .count = 2, .split_ways = voc_ways,
                         .parallel = true, .room_designators = prac,
                         .teacher_pool = &inf_pool });
            p.push_back({ .subject = prog, .count = 1, .duration = 2,
                         .split_ways = voc_ways, .parallel = true,
                         .room_designators = prac, .teacher_pool = &prog_pool });
            if (level == 3) {
              p.push_back({ .subject = niem, .count = 2, .split_ways = lang_ways,
                           .parallel = true, .room_designators = jez,
                           .teacher_pool = &niem_pool });
              p.push_back({ .subject = siec, .count = 1, .split_ways = voc_ways,
                           .parallel = true, .room_designators = prac,
                           .teacher_pool = &siec_pool });
            }
            if (level == 4) {
              p.push_back({ .subject = siec, .count = 1, .split_ways = voc_ways,
                           .parallel = true, .room_designators = prac,
                           .teacher_pool = &siec_pool });
              p.push_back({ .subject = adm, .count = 1, .duration = 2,
                           .split_ways = voc_ways, .parallel = true,
                           .room_designators = prac, .teacher_pool = &adm_pool });
              p.push_back({ .subject = sysm, .count = 1, .split_ways = voc_ways,
                           .parallel = true, .room_designators = prac,
                           .teacher_pool = &sysm_pool });
            }
            if (level == 5) {
              p.push_back({ .subject = adm, .count = 1, .duration = 2,
                           .split_ways = voc_ways, .parallel = true,
                           .room_designators = prac, .teacher_pool = &adm_pool });
              p.push_back({ .subject = elek, .count = 1, .split_ways = voc_ways,
                           .parallel = true, .room_designators = prac,
                           .teacher_pool = &elek_pool });
              p.push_back({ .subject = bazy, .count = 1, .duration = 2,
                           .split_ways = voc_ways, .parallel = true,
                           .room_designators = prac, .teacher_pool = &bazy_pool });
            }
          }
          return p;
          };

        // Divisions per year and their group counts, matching the real histogram:
        // 19 divisions × 2 groups, 3 × 3, 12 × 5 (the 5-way splits are vocational).
        struct DivSpec { int level; std::vector<int> group_counts; };
        std::vector<DivSpec> by_year = {
            {1, {2, 2, 2, 2, 2, 3, 3}},        // year 1: 7 divisions
            {2, {2, 2, 2, 2, 3}},              // year 2: 5
            {3, {2, 2, 5, 5, 5, 5, 5}},        // year 3: 7
            {4, {2, 2, 2, 2, 2, 5, 5, 5, 5}},  // year 4: 9
            {5, {2, 2, 2, 5, 5, 5}},           // year 5: 6
        };
        if (scale.mega_features) {
          // ~35% more divisions across the years (46 total, ~1700 lessons).
          by_year[0].group_counts.insert(by_year[0].group_counts.end(), { 2, 2, 3 });
          by_year[1].group_counts.insert(by_year[1].group_counts.end(), { 2, 2, 3 });
          by_year[2].group_counts.insert(by_year[2].group_counts.end(), { 2, 3 });
          by_year[3].group_counts.insert(by_year[3].group_counts.end(), { 3, 3 });
          by_year[4].group_counts.insert(by_year[4].group_counts.end(), { 2, 3 });
        }

        int ordinal = 0;
        for (const DivSpec& ys : by_year) {
          char suffix = 'a';
          for (int gcount : ys.group_counts) {
            const std::string name = std::to_string(ys.level) + std::string(1, suffix);
            Id c = b.AddDivision(name, years[ys.level - 1]);
            // One OPEN split per division carries its x/N groups (the real export's
            // structure; membership follows the plan).
            Id split = b.AddSplit(c, "grupy", SplitKind::kOpen);
            std::vector<Id> groups;
            const std::string label = std::to_string(gcount);  // "1/2".."N/N" names
            for (int g = 1; g <= gcount; ++g) {
              groups.push_back(b.AddGroup(c, std::to_string(g) + "/" + label,
                split));
            }
            std::vector<SubjectPlan> plans = plans_for(ys.level, gcount);
            // The first two divisions run PE in a FIXED gender split instead of
            // the language groups, so the preset exercises rule 3 of
            // model/structure.h (fixed never parallel with other splits).
            std::vector<Id> gender_groups;
            if (ordinal < 2) {
              Id pe_split = b.AddSplit(c, "wf", SplitKind::kFixed);
              gender_groups = { b.AddGroup(c, "dz", pe_split),
                               b.AddGroup(c, "ch", pe_split) };
              for (SubjectPlan& plan : plans) {
                if (plan.subject == wf) {
                  plan.groups_override = &gender_groups;
                  plan.split_ways = 2;
                }
              }
            }
            AddDivisionLessons(b, c, groups, plans, ordinal);
            ++ordinal;
            ++suffix;
          }
        }

        // A few external availability constraints (part of the problem, not a
        // schedule): a teacher and a room are unavailable for a stretch.
        b.AddExternalBlock(BlockTarget::kTeacher, mat_pool[0], "Rada pedagogiczna",
          0, 0, 2);
        b.AddExternalBlock(BlockTarget::kRoom, b.m.rooms[sala.size()].id,
          "Serwis pracowni", 4, 8, 6);

        b.m.preferences.push_back({ PreferenceKind::kMaxLessonsPerDay, kNoId, kNoId,
                                   kNoId, /*weight=*/30, /*param=*/9 });

        if (scale.mega_features) {
          // --- The capability layer: every solver feature, feasibility-safe. ---
          auto division_id_of = [&] (size_t ordinal) {
            return b.m.divisions[ordinal].id;
            };
          // Nth whole-division lesson of (division, subject).
          auto lesson_of = [&] (Id division, Id subject, int nth) -> LessonInstance* {
            for (LessonInstance& lesson : b.m.lessons) {
              if (lesson.subject_id != subject) continue;
              bool of_division = false;
              for (const Participant& p : lesson.participants) {
                of_division |= p.division_id == division;
              }
              if (of_division && nth-- == 0) return &lesson;
            }
            return nullptr;
            };
          // Locked lessons (konkretne godziny): two pins on distinct slots.
          // Day 1, not day 0: the production Rada block occupies mat_pool[0]'s
          // Monday morning, and the seed decides which teacher this lesson gets.
          LockFirstLesson(b, division_id_of(0), mat, 1, 1, b.m.rooms[0].id);
          LockFirstLesson(b, division_id_of(1), pol, 2, 3, b.m.rooms[1].id);
          // Lesson links: bez-bloków recipe on historia (DIFFERENT_DAY), a
          // SAME_DAY pairing, and an ordered CONSECUTIVE maths block.
          for (size_t ordinal : {size_t{ 0 }, size_t{ 2 }, size_t{ 4 }}) {
            const Id division = division_id_of(ordinal);
            LessonInstance* h1 = lesson_of(division, hist, 0);
            LessonInstance* h2 = lesson_of(division, hist, 1);
            if (h1 != nullptr && h2 != nullptr) {
              b.m.lesson_links.push_back({ b.NewId(), LessonLinkKind::kDifferentDay,
                                          {h1->id, h2->id}, false });
            }
          }
          {
            LessonInstance* w = lesson_of(division_id_of(0), wych, 0);
            LessonInstance* r = lesson_of(division_id_of(0), rel, 0);
            if (w != nullptr && r != nullptr) {
              b.m.lesson_links.push_back({ b.NewId(), LessonLinkKind::kSameDay,
                                          {w->id, r->id}, false });
            }
            LessonInstance* m1 = lesson_of(division_id_of(2), mat, 0);
            LessonInstance* m2 = lesson_of(division_id_of(2), mat, 1);
            if (m1 != nullptr && m2 != nullptr) {
              b.m.lesson_links.push_back({ b.NewId(), LessonLinkKind::kConsecutive,
                                          {m1->id, m2->id}, /*ordered=*/true });
            }
          }
          // Religion at the day edges for the first four divisions.
          for (size_t ordinal = 0; ordinal < 4; ++ordinal) {
            LessonInstance* r = lesson_of(division_id_of(ordinal), rel, 0);
            if (r != nullptr) r->edge = EdgePlacement::kEither;
          }
          // Soft daily-load band: mild pressure toward even days.
          b.m.daily_load_rules.push_back({ kNoId, kNoId, /*min=*/0, /*max=*/0,
            /*target=*/0, /*allowed_deviation=*/2,
            /*deviation=*/5, /*imbalance=*/3,
            /*overload=*/8, /*underload=*/4 });
          // Prefer-early for the youngest year.
          b.m.preferences.push_back({ PreferenceKind::kPreferEarly,
                                     b.m.years.front().id, kNoId, kNoId,
            /*weight=*/2, /*param=*/0 });
          // Rule philosophy: comfort dialed up, Dobry Plan flags as overrides.
          b.m.rule_config.overrides = {
              {"subject_once", RuleMode::kHard, 0, 0, kNoId, kNoId, hist},
              {"anti_split_shift", RuleMode::kSoft, /*weight=*/60, /*param=*/2},
              {"single_lesson_day", RuleMode::kSoft, /*weight=*/25},
              {"few_days", RuleMode::kSoft, /*weight=*/40, /*param=*/3, kNoId,
               kNoId, kNoId, rel_pool[0]},
              {"teacher_gaps", RuleMode::kSoft, /*weight=*/200, 0, kNoId, kNoId,
               kNoId, mat_pool[0]},
          };
          // More availability shapes: a division off-site block and a group block.
          b.AddExternalBlock(BlockTarget::kDivision, division_id_of(0), "Basen",
            2, 0, 2);
          b.AddExternalBlock(BlockTarget::kGroup, b.m.groups.front().id,
            "Zajęcia dodatkowe", 3, 12, 2);
        }
        return std::move(b.m);
      }

    }  // namespace

    SchoolModel BuildProduction(Builder& b) {
      return BuildTechnikum(b, { "Technikum (pełne)", 18, 8, 4, 7,
        /*pool_scale=*/1.0, /*mega_features=*/false });
    }

    SchoolModel BuildMega(Builder& b) {
      return BuildTechnikum(b, { "Technikum (mega)", 26, 12, 6, 10,
        /*pool_scale=*/1.4, /*mega_features=*/true });
    }

  }  // namespace demo_detail
}  // namespace arrango
