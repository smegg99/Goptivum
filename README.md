# Goptivum

Open-source replacement for VULCAN's Optivum school timetable software,
being rebuilt from scratch as a set of services. The previous iteration —
a Go scraper/viewer for published Optivum schedules — lives in the git
history before this restructure.

The first and currently only component is the **timetable solver service**
under `backend/services/solver/`: it generates and scores school timetables
instead of just displaying them. It was developed under the working name
*Arrango*, which still shows in the proto packages, Go module path, and
`ARRANGO_*` environment variables.

Built with C++20, OR-Tools 9.15 (CP-SAT), gRPC, Protobuf, Go (Gin), and
Nuxt 4 (Vue 3, Vuetify).

Status: working prototype, not production-ready. All demo data is synthetic
and generated inside the solver; nothing depends on real school exports.

## Layout

```text
backend/services/solver/
  solver/    C++20 OR-Tools CP-SAT solver service (gRPC)
  backend/   Go/Gin HTTP+SSE bridge, VULCAN Optivum HTML import
  frontend/  test SPA (Nuxt 4, Vue 3, Vuetify) for driving the solver
  proto/     Protobuf definitions (arrango/v1)
  justfile   build, test, and run recipes
frontend/    placeholder, nothing here yet
```

The solver's design docs and demo data are development-only and
deliberately not committed.

## What the solver does

- Models configurable days/periods, year levels, divisions, group splits of
  any arity (open planning splits and fixed-membership splits like gender
  PE), parallel group blocks, teachers, rooms and room types, two-period
  lessons, locked lessons, and external fixed blocks (unavailability).
- Enforces hard constraints via CP-SAT: exactly-one placement,
  teacher/division/group/room no-overlap, room eligibility, locked
  placements, aligned parallel blocks, external blocks. Impossible
  placements are pruned before they ever become variables.
- Optimizes weighted soft preferences: student and teacher gaps, late
  lessons, subject splits and broken blocks, teacher room changes,
  single-lesson teacher days, split shifts, stability against previous
  placements, prefer-early and max-lessons-per-day. Student-facing
  penalties scale with year priority.
- Every comfort rule is a three-position switch — `hard`, `soft`, `off` —
  with per-entity overrides (per subject/teacher/division/year) and named
  profiles built into the core. Mistyped configs are refused at preflight,
  never silently defaulted.
- Rates schedules on an absolute 0–100 scale with per-division, per-year,
  per-teacher, and overall breakdowns; every result carries an
  ERRORS/WARNINGS/PRISTINE verdict.
- Validates schedules with a pure, CP-SAT-free module covering 14 hard
  conflict kinds; every streamed solution is re-validated and re-scored
  before it leaves the solver.
- Imports real VULCAN Optivum HTML exports (zip upload): the parser in
  `backend/optivum` extracts demand data and original placements, which are
  validated, scored, and used as the baseline for re-solving — the solver
  never returns a schedule worse than the imported one.
- Streams improved solutions live over gRPC server-streaming; the Go
  backend forwards them to clients over SSE and supports cancellation.

## Prerequisites

- gcc with C++20, CMake ≥ 3.24, Ninja, [just](https://github.com/casey/just)
- OR-Tools 9.15 C++ binary bundle at `~/.local/opt/or-tools`, which ships
  its own protobuf, abseil, and `protoc`
- gRPC C++ built from source **against the bundle's protobuf/abseil**,
  installed at `~/.local/opt/grpc`
- GoogleTest (system package)
- Go ≥ 1.26, `protoc-gen-go`, `protoc-gen-go-grpc` (in `~/go/bin`)

Do not link the distro's gRPC/protobuf/abseil into the solver: the bundle
ships different versions of the same libraries and the mix crashes at
runtime. The solver build pins both prefixes and builds `Release` for the
same reason.

## Build and test

Everything generated lands in the gitignored `build/` inside the service
directory. From `backend/services/solver/`:

```sh
just solver-test        # build the solver and run the C++ test suite
just backend-test       # generate Go proto code and run the Go tests
just frontend-install   # install test SPA dependencies
```

`just doctor` checks that the required tools are on the path.

## Running

```sh
just run-solver      # gRPC solver service on 127.0.0.1:50061
just run-backend     # HTTP+SSE bridge on 127.0.0.1:18080
just run-frontend    # test SPA dev server on http://localhost:3000
```

or all three at once with prefixed logs: `just dev`.

The backend exposes the HTTP API, including a simple name-based solve flow
(`POST /api/simple/solve` with raw demand data, poll
`GET /api/simple/jobs/{id}`) and the advanced endpoints:

```text
GET  /api/school?preset=DEMO_PRESET_MEDIUM&seed=42
POST /api/import              multipart zip of a VULCAN Optivum HTML export
POST /api/validate            {model, snapshot}
POST /api/score               {model, snapshot}
POST /api/jobs                {preset|model, seed, params}
GET  /api/jobs/:id
POST /api/jobs/:id/cancel
GET  /api/jobs/:id/events     (SSE)
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `ARRANGO_HTTP_ADDR` | `:8080` | Backend listen address (`just run-backend` uses `127.0.0.1:18080`). |
| `ARRANGO_SOLVER_ADDR` | `127.0.0.1:50061` | Solver gRPC address the backend dials. |
| `ARRANGO_LOG` | unset | When set, the solver narrates each solve. |

Penalty weights, year priorities, the late-lesson threshold, and the gap
cap are part of the `SchoolModel` proto (`Weights`, zero = built-in
default) and can be overridden per solve request.

## License

See [LICENSE](LICENSE).
