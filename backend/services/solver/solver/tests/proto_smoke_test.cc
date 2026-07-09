// tests/proto_smoke_test.cc

#include <gtest/gtest.h>

#include "arrango/v1/school.pb.h"
#include "arrango/v1/service.grpc.pb.h"

TEST(ProtoSmoke, RoundTrip) {
  arrango::v1::SchoolModel m;
  m.set_name("demo");
  auto* d = m.add_days();
  d->set_id(1);
  d->set_period_count(12);
  std::string bytes;
  ASSERT_TRUE(m.SerializeToString(&bytes));
  arrango::v1::SchoolModel back;
  ASSERT_TRUE(back.ParseFromString(bytes));
  EXPECT_EQ(back.days(0).period_count(), 12u);
}

TEST(ProtoSmoke, GrpcServiceSymbolsExist) {
  // Links the generated service stub to prove grpc codegen + linkage works.
  EXPECT_NE(arrango::v1::SolverService::service_full_name(), nullptr);
}
