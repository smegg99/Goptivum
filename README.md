# Goptivum Server

The daemon that owns identity and all application data for one school. It
takes a school's existing Optivum (VULCAN) files, keeps each timetable as a
versioned document, and hands work to an Arrango node.

Module `github.com/smegg99/goptivum`. Binary `goptivum-server`.

## Prerequisites

| Tool | Needed for |
|---|---|
| Go 1.27 | everything |
| pnpm | `just contracts`, and therefore `just check` |
| Docker | `just infra-up`, and therefore `just test-infra` |

The contract tooling lives in its own workspace, so a fresh checkout runs:

```sh
cd contracts && pnpm install
```

Without it `just check` stops at `check-generated`, because the Redocly CLI is
not there to bundle the contract.

## The three verbs

```sh
just build   # bin/goptivum-server, stamped with VERSION and the commit
just run     # run what build produced
just dev     # go run, no artifact
```

## goptivum-server

```
goptivum-server serve      run until SIGINT or SIGTERM
goptivum-server status     ask a running server what it is and whether it is ready
goptivum-server --version  what this build serves and what it demands of a client
```

A misuse exits 2 and a failure of the work itself exits 1, so a script can tell
them apart.

### Configuration

`config.yaml` owns the settings. It is validated against the CUE schema in
`backend/common/config/config.cue`, and a first run writes the defaults so a
fresh install has a file to edit rather than an error to read. The Go types are
generated from that schema and are never hand-edited.

```sh
goptivum-server serve --config /etc/goptivum/config.yaml
```

Secrets are references, not values in the file. `storage.database.dsn` and the
bucket keys default to `@{env?:GOPTIVUM_DATABASE_URL}`,
`@{env?:GOPTIVUM_STORAGE_ACCESS_KEY}` and `@{env?:GOPTIVUM_STORAGE_SECRET_KEY}`,
and a `.env` beside the binary is read on start. None of them is a flag,
because a command line is visible in the process list and in shell history.

Four flags exist, for what someone changes for one run. A flag wins only when
it is given, so a default never replaces a value the school wrote down:

| Flag | Overrides |
|---|---|
| `--config` | where the file is; `GOPTIVUM_CONFIG` names it too |
| `--addr` | `server.address` |
| `--verbose` | `logging.verbose` |
| `--no-color` | `logging.no_color` |

Documents live in a bucket when `storage.documents.endpoint` is set and in
`storage.documents.dir` otherwise, which is what a single-box or air-gapped
school runs.

Logs go to stderr through S99Logger: coloured when stderr is a terminal, plain
when it is a pipe or a journal, and optionally to a rotating file when
`logging.enable_files` is on. Every line carries a stable event id beside the
sentence, so `id=logs.serverStarted` is what a log search matches whatever
language the sentence is in.

There is no identity provider yet. Until one is configured every route that
needs a session stays unregistered, which is deliberate: mounting them without
a session check would mount them open to anyone who can reach the port.
`/api/info`, `/api/v1/health`, `/api/v1/ready` and `/api/v1/version` answer
today.

## plaprobe

Looks inside a school's `.pla` without printing the school.

```
plaprobe census FILE   every element, its count, and the names of its attributes
plaprobe tree FILE     where each element sits and what type each attribute holds
plaprobe xml FILE      the decoded document, and nothing else, for a redirect
```

`census` and `tree` report shape and never a value, so their output can go in a
bug report. `xml` writes what is inside, which for a real file is a school's
personal data.

```sh
just pla-probe census tests/data/plan.pla
```

## Checks

```sh
just check    # fmt-check, vet, test, check-generated
```

`just test` alone runs the Go tests. Two families of test skip rather than fail
when their inputs are absent:

- **Corpus tests** read real school files from `tests/data/`, which is
  gitignored and empty in a clean checkout. A real Optivum export carries
  teacher names, and a git history is forever.
- **Integration tests** (`-run Infra`) need Postgres and MinIO. Start them with
  `just infra-up` and run `just test-infra`.

## Language

Everything this server writes for a person to read is localized with
S99Locale: its log and what its command line prints. `application.language`
picks it, `en` and `pl` exist, and `i18n/locales/` is where the messages are
written.

```sh
just locales        # build the catalogs and copy them to the Go consumer
just check-locales  # fails when a catalog or its copy is stale
```

Use `--lang en|pl` to override the server command's language for one run,
including its logs. Help, version output, and early usage errors use
`GOPTIVUM_LANG`, then `LC_ALL`, `LC_MESSAGES`, `LANG`, and finally English
when no `--lang` is given. `plaprobe` uses the same environment detection.
`C` and `POSIX` select English; unsupported locales fall back to English.
Help does not load or create configuration files.

```sh
go run ./backend --lang pl --help
go run ./backend --lang en status
```

A response never carries a sentence. `Error.message` is a key such as
`error.notFound`, and the client resolves it from its own catalogue in the
language the person reading it chose. Command names and flag names remain
stable in every language. Original dependency diagnostics retain their text
inside localized error messages.

## The contract

`contracts/backend/` is the source of truth. Nothing downstream is ever
hand-edited: the bundle, the generated Go boundary and any client are
regenerated from the authored tree.

```sh
just contracts        # lint the authored tree, bundle it to dist/openapi.yaml
just generate         # the configuration types and the Go server boundary
just check-generated  # fails when a generated file does not match its source
```

`/api/info` is unversioned and frozen. A client has to learn which version a
server speaks before it can call a versioned route, so the discovery path
cannot itself carry a version. Its four fields are `product`, `api_version`,
`min_client_version` and `server_version`, and `product` is `goptivum`.

## Layout

```
backend/cmd/           the command tree; one file per command
backend/common/config/ the CUE schema and the types generated from it
backend/common/logger/ the logger and its event catalog
backend/common/messages/ the generated locale catalogs and accessors
i18n/                  the canonical locale sources, one file per namespace
backend/ui/            one palette, one set of styles, every line the programs print
backend/api/v1/        the HTTP boundary; gen/ is generated, never edited
backend/core/plans/    which plans exist and the timeline of versions each has
backend/core/documents/ protobuf, then zstd, addressed by the hash of the bytes
backend/formats/       a school's own files in, a solvable model out
backend/migrations/    one migration, edited in place
contracts/backend/     the authored OpenAPI tree and its bundle
tests/                 data/ is real and gitignored; fixtures/ and golden/ are committed
```
