# Solver API reference

The solver is a gRPC service, `arrango.v1.SolverService`, defined in
`proto/arrango/v1/{service,school,schedule}.proto`. Those `.proto` files are the
authoritative field-level reference; this document explains the RPCs, the
streaming protocol, the config, and the semantics that aren't obvious from the
field names.

Generate client stubs from the three proto files (Go stubs are produced by
`just proto-go` into `backend/gen/arrangov1`). The Go/Gin backend
(`backend/`) also wraps this service over HTTP+SSE if you prefer JSON —
`ModelToProto`/`ModelFromProto` use protojson, so message field names in JSON
are the camelCase forms of the proto fields.

## RPCs

```proto
service SolverService {
  rpc GetDemoSchool(DemoRequest) returns (SchoolModel);
  rpc Validate(ValidateRequest) returns (ValidationReport);
  rpc Score(ScoreRequest) returns (ScoreReport);
  rpc Solve(SolveRequest) returns (stream SolveUpdate);
}
```

### GetDemoSchool

Returns a synthetic `SchoolModel` for a `DemoPreset` (`TINY`, `SMALL`, `MEDIUM`,
`FULL`, `PRODUCTION`). Deterministic per `(preset, seed)`; the seed only rotates
names/teacher assignments, never structure. `PRODUCTION` is a ~34-division,
~1200-lesson technikum with no pre-placed schedule — raw problem data to solve
from scratch.

### Validate

`ValidateRequest{ model, snapshot }` → `ValidationReport`. Checks the snapshot
against every hard rule independently of the solver (unknown references, missing
or duplicate lessons, room eligibility/capacity, teacher/room/student overlap,
parallel-block breaks, moved locks, external-block overlaps, daily-load). This
is the source of truth for "is this schedule valid" — `report.valid` is true iff
`conflicts` is empty. Each `Conflict` carries a structured `locus` (typed
entities, lesson refs with placement, time spans) so a UI can point at exactly
what and where.

### Score

`ScoreRequest{ model, snapshot }` → `ScoreReport`. Computes the soft-penalty
breakdown: `overall_quality` (0..100), per-division / per-year / per-teacher
scores, a global per-category `PenaltyItem` list, and located `SoftIssue`s.
`total_penalty` equals the CP-SAT objective on a hard-valid schedule (guaranteed
by the drift-guard test).

### Solve

`SolveRequest{ model, params?, config? }` → **server-streaming** `SolveUpdate`.
Resolves the runtime config (explicit `config` wins; otherwise the legacy
`params` are mapped into it), runs the pipeline, and streams updates until done.

## Solve streaming protocol

Every `SolveUpdate` has a `phase`, a `status`, `wall_time_seconds`, and the
fully-resolved `config` that produced it (echoed on *every* update so any stored
result records its exact configuration). Phases:

| Phase | Carries | When |
|---|---|---|
| `STARTED` | config only | Once, immediately. |
| `SOLUTION` | `snapshot`, `objective`, `score`, `validation`, `best_bound` | A new best feasible schedule, or the imported baseline. |
| `PROGRESS` | `objective`, `best_bound`, `solutions_found` | ~1 Hz heartbeat while searching; no snapshot. |
| `DONE` | final `snapshot` + `score` + `validation` + `status` | Once, last. |

Baseline behavior: if every lesson in the request carries a `previous_placement`
and that schedule is hard-valid, it is streamed first as a `SOLUTION` and used
as the incumbent — the final result never regresses below it.

`SolveStatus`: `OPTIMAL`, `FEASIBLE`, `INFEASIBLE`, `CANCELLED`, `TIMEOUT`,
`ERROR`. Large models return `FEASIBLE` (construct + LNS never proves
optimality). Cancel a solve by cancelling the gRPC call; the best solution so
far is returned as `CANCELLED`.

Note: only `SOLVE_MODE_FULL_PIPELINE` (the default / `UNSPECIFIED`) is
implemented. Other modes stream a single `DONE` with `status = ERROR`.

## SolverConfig

See the [README configuration table](../README.md#configuration) for which
fields are effective versus carried-but-unused. Conventions: zero means "server
default"; booleans are phrased `disable_*` so the zero value keeps the feature
on; `weights` is copied from `model.weights` when unset so the echo always
records what was used.

## Data messages (school.proto, schedule.proto)

Exact fields are in the `.proto` files. The semantically important ones:

- **`SchoolModel`** — flat and ID-referenced (no nested object graphs): `days`,
  `periods`, `years`, `divisions`, `groups`, `teachers`, `subjects`, `rooms`,
  `lessons`, `external_blocks`, `weights`, `preferences`, `daily_load_rules`.
- **`LessonInstance`** — the schedulable unit. `participants` (one or more
  `{division_id, group_id}`; `group_id` 0/absent = whole division);
  `allowed_room_designators` / `disallowed_room_designators` (empty allowed =
  any room; disallowed always wins); `parallel_block_id` (lessons sharing it
  start together); `locked` + `locked_placement`; `has_previous` +
  `previous_placement` (a stability hint / baseline, never a room constraint).
- **`Placement`** — `{ day_id, start_period, room_id }`. `room_id` 0 = no room.
- **`ScheduleSnapshot`** — `lessons: [{ lesson_id, placement }]`. One entry per
  placed lesson; this is what `Solve` produces and `Validate`/`Score` consume.
- **`Room`** — has a `designator` (canonical short code, exact-match) separate
  from its display `name`; eligibility is by designator.
- **`ExternalBlock`** — a `{ target, target_id }` (division/group/teacher/room)
  unavailable at `{ day_id, start_period, duration }`.
- **`Weights`** — soft-penalty weights (student/teacher gaps, late lessons,
  subject split, block break, room change, stability, ...). Student-facing
  weights are scaled by the division's year priority.
- **`Conflict` / `SoftIssue`** — both carry a structured locus: `entities`
  (typed `EntityRef`), `lessons` (`LessonRef` with placement), and `spans`
  (`TimeSpan`), so issues are machine-locatable, not just a string.

## Minimal usage

```
GetDemoSchool(preset=PRODUCTION)              -> SchoolModel      (raw problem)
Solve(model, config{ total_time_seconds=180 }) -> stream of SolveUpdate
     ... take the last DONE update's snapshot ...
Validate(model, snapshot)                      -> ValidationReport (should be valid)
Score(model, snapshot)                         -> ScoreReport      (penalty breakdown)
```
