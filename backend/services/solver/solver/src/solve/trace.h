// src/solve/trace.h

#pragma once

namespace arrango {
  namespace trace {

    // Developer-facing pipeline tracing. Off unless the ARRANGO_LOG environment
    // variable is set (checked once). When on, the solve narrates each stage —
    // model size, which path it took and why, construct/LNS progress, and the final
    // outcome — to stderr, so a run is legible instead of a black box.
    bool Enabled();

    // printf-style trace line to stderr, prefixed with "[arrango] ". No-op when
    // tracing is disabled, so call sites need no guard.
    void Line(const char* fmt, ...) __attribute__((format(printf, 1, 2)));

    // RAII scope that silences trace output on the current thread while alive.
    // Keeps recursive LNS sub-solves from flooding the log with one "solve:"
    // block per neighborhood — the LNS loop logs the neighborhood itself.
    class ScopedQuiet {
    public:
      ScopedQuiet();
      ~ScopedQuiet();
      ScopedQuiet(const ScopedQuiet&) = delete;
      ScopedQuiet& operator=(const ScopedQuiet&) = delete;
    };

  }  // namespace trace
}  // namespace arrango
