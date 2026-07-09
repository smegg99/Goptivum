// tests/structure_test.cc

#include <gtest/gtest.h>

#include "model/structure.h"

namespace arrango {
  namespace {

    // 1A: open split "lang" {1/2, 2/2}, open split "inf" {i1}, fixed split "pe"
    // {girls, boys}, split-less group "loose". 1B: empty, for cross-division
    // checks. One test per line of the overlap rulebook.
    SchoolModel M() {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A", .year_id = 10},
                     {.id = 21, .name = "1B", .year_id = 10} };
      m.splits = { {.id = 5, .name = "lang", .division_id = 20,
                   .kind = SplitKind::kOpen},
                  {.id = 6, .name = "inf", .division_id = 20,
                   .kind = SplitKind::kOpen},
                  {.id = 7, .name = "pe", .division_id = 20,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 30, .name = "1/2", .division_id = 20, .split_id = 5},
                  {.id = 31, .name = "2/2", .division_id = 20, .split_id = 5},
                  {.id = 32, .name = "i1", .division_id = 20, .split_id = 6},
                  {.id = 33, .name = "girls", .division_id = 20, .split_id = 7},
                  {.id = 34, .name = "boys", .division_id = 20, .split_id = 7},
                  {.id = 35, .name = "loose", .division_id = 20} };
      return m;
    }

    Participant P(Id division, Id group = kNoId) { return { division, group }; }

    TEST(Structure, SameSplitDifferentGroupsDontShare) {
      SchoolModel m = M();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_FALSE(SharesStudents(m, ix, P(20, 30), P(20, 31)));  // 1/2 vs 2/2
      EXPECT_FALSE(SharesStudents(m, ix, P(20, 33), P(20, 34)));  // girls vs boys
    }

    TEST(Structure, OpenVsOpenAcrossSplitsDontShare) {
      SchoolModel m = M();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_FALSE(SharesStudents(m, ix, P(20, 30), P(20, 32)));  // lang vs inf
      EXPECT_FALSE(SharesStudents(m, ix, P(20, 30), P(20, 35)));  // vs split-less
    }

    TEST(Structure, FixedVsOtherSplitShares) {
      SchoolModel m = M();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_TRUE(SharesStudents(m, ix, P(20, 33), P(20, 30)));  // girls vs 1/2
      EXPECT_TRUE(SharesStudents(m, ix, P(20, 33), P(20, 35)));  // girls vs loose
      EXPECT_TRUE(SharesStudents(m, ix, P(20, 30), P(20, 33)));  // symmetric
    }

    TEST(Structure, WholeDivisionAndSameGroupShare) {
      SchoolModel m = M();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_TRUE(SharesStudents(m, ix, P(20), P(20, 30)));
      EXPECT_TRUE(SharesStudents(m, ix, P(20), P(20)));
      EXPECT_TRUE(SharesStudents(m, ix, P(20, 30), P(20, 30)));
      EXPECT_TRUE(SharesStudents(m, ix, P(20, 35), P(20, 35)));
    }

    TEST(Structure, DifferentDivisionsNeverShare) {
      SchoolModel m = M();
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_FALSE(SharesStudents(m, ix, P(20), P(21)));
      EXPECT_FALSE(SharesStudents(m, ix, P(20, 33), P(21)));
    }

    TEST(Structure, StreamCounts) {
      SchoolModel m = M();
      ModelIndex ix = ModelIndex::Build(m);
      // 1A: 4 open groups (1/2, 2/2, i1, loose) x fixed tuple {girls|boys} = 8.
      // 1B: no groups -> 0, meaning one whole-class stream.
      EXPECT_EQ(StreamCountPerDivision(m, ix), (std::vector<int64_t>{8, 0}));
    }

    TEST(Structure, StreamCountsFixedOnlyDivision) {
      SchoolModel m;
      m.divisions = { {.id = 20, .name = "1A", .year_id = 10} };
      m.splits = { {.id = 7, .name = "pe", .division_id = 20,
                   .kind = SplitKind::kFixed} };
      m.groups = { {.id = 33, .name = "girls", .division_id = 20, .split_id = 7},
                  {.id = 34, .name = "boys", .division_id = 20, .split_id = 7} };
      ModelIndex ix = ModelIndex::Build(m);
      EXPECT_EQ(StreamCountPerDivision(m, ix), (std::vector<int64_t>{2}));
    }

  }  // namespace
}  // namespace arrango
