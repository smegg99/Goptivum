// src/solve/trace.cc

#include "solve/trace.h"

#include <cstdarg>
#include <cstdio>
#include <cstdlib>

namespace arrango {
  namespace trace {

    namespace {
      thread_local int g_suppress = 0;
    }

    bool Enabled() {
      static const bool on = std::getenv("ARRANGO_LOG") != nullptr;
      return on;
    }

    ScopedQuiet::ScopedQuiet() { ++g_suppress; }
    ScopedQuiet::~ScopedQuiet() { --g_suppress; }

    void Line(const char* fmt, ...) {
      if (!Enabled() || g_suppress > 0) return;
      std::fputs("[arrango] ", stderr);
      va_list ap;
      va_start(ap, fmt);
      std::vfprintf(stderr, fmt, ap);
      va_end(ap);
      std::fputc('\n', stderr);
      std::fflush(stderr);
    }

  }  // namespace trace
}  // namespace arrango
