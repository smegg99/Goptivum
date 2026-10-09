# Goptivum Server

The daemon that owns one school's accounts and data. It imports the school's
Optivum (VULCAN) files and `.gpf` project files, keeps each timetable project
as versioned documents, and sends solves to Arrango nodes. Goptivum Desktop is
its only client today.

```
Goptivum Desktop --HTTP + bearer token--> Goptivum Server --gRPC + pinned mTLS--> Arrango node
  (../Goptivum-Desktop)                   (this repo)                             (../Arrango)
```

## Prerequisites

| Tool | Needed for |
|---|---|
| Go 1.27 | everything; `cue`, `oapi-codegen` and `loc` run as `go tool` from `go.mod` |
| a `../Arrango` checkout | building at all (`go.mod` replaces `github.com/smegg99/arrango` with it), and `just proto-go`, which reads its `proto/` |
| pnpm, then `cd contracts && pnpm install` | Redocly, for `just contracts`, `just generate` and `just check-generated` |
| Python 3 | `scripts/*.py`: `just locales`, `check-locales`, `proto-go`, `shared-contracts` |
| protoc 33.1 at `~/.local/opt/or-tools` (or `GOPTIVUM_PROTOC_PREFIX`) and `protoc-gen-go` v1.36.11 | `just proto-go` and `proto-check`, so `just generate` too; the script checks both versions |
| Docker | `just infra-up`, `test-infra`, a disposable Kratos for `test-kratos` |

## Build and run

```sh
just build   # bin/goptivum-server, stamped with VERSION, the commit and the build time
just run     # build, then serve
just dev     # go run ./backend serve, no artifact
```

## Commands

```
goptivum-server serve                              run until SIGINT or SIGTERM
goptivum-server status                             ask a running server what it is and whether it is ready
goptivum-server arrango pair URI [--label NAME]    pair an Arrango node from its one-time link
goptivum-server arrango nodes                      list the paired nodes
goptivum-server arrango revoke LABEL-OR-FINGERPRINT
goptivum-server users set-role IDENTIFIER admin|planner
goptivum-server --version                          this build, its API version and the oldest client it accepts
```

`users set-role` is the operator's way back in: it changes an account's role
through the same last-admin guard as the Users page.

A misuse exits 2 and a failure of the work itself exits 1, so a script can
tell them apart.

Five flags exist, for what someone changes for one run. A flag wins only when
it is given, so a default never replaces a value the school wrote down:

| Flag | Overrides |
|---|---|
| `--config` | where `config.yaml` is; `GOPTIVUM_CONFIG` names it too; default `./config.yaml` |
| `--addr` | `server.address` |
| `--verbose` | `logging.verbose` |
| `--no-color` | `logging.no_color` |
| `--lang en\|pl` | `application.language`, for this run's output and logs |

## Configuration

Two files, both validated against CUE schemas in `backend/common/config/`:

- `config.yaml` (`config.cue`) holds the settings a person writes: address,
  database, document storage, Kratos, logging. A first run writes the
  defaults. Unknown keys are refused.
- `settings.yaml` (`settings.cue`) holds the runtime settings an
  administrator changes from the desktop (Backend > Server and Security):
  sign-in lockout, public addresses, upload size, request rate, Trash days,
  Backups and results caps. The server creates it beside `config.yaml` and
  rewrites it on each change. It reads the file at start only, so edit it by
  hand only while the server is stopped.

Secrets are references, not values: `storage.database.dsn`, the bucket keys
and the escrow passphrase default to `@{env?:GOPTIVUM_DATABASE_URL}`,
`@{env?:GOPTIVUM_STORAGE_ACCESS_KEY}`, `@{env?:GOPTIVUM_STORAGE_SECRET_KEY}`
and `@{env?:GOPTIVUM_ESCROW_PASSPHRASE}`. They resolve from the environment,
a `.env` in the working directory, or the keyring. None of them is a flag,
because a command line is visible in the process list and in shell history.

## What it keeps

- **Projects**: a school's timetable plan. Every account reads every live
  project; only its owner changes or solves it, anyone may fork it, and an
  administrator can give it to another account.
- **Versions**: each save of a project is a new version, kept up to the
  project's Backups cap. A named checkpoint is a version that is kept.
- **Trash**: a deleted project, checkpoint or result goes to Trash, where its
  owner can restore it; an hourly purge removes what is older than the Trash
  days setting (30 by default).
- **Results**: a finished solve is kept as a result of its project, not as a
  new version.
- **Exports**: a version or result as `.gpf` or Optivum HTML, built once and
  kept.

## Identity

Accounts live in Ory Kratos. The server proxies Kratos at `/kratos/*`, so a
school exposes one port, and the desktop signs in through it. **Kratos's
`serve.public.base_url` must be this server's public origin with a trailing
`/kratos/`**; a mismatch shows up as a 404 on submit.

`identity.admin_url` must never be reachable beyond loopback: anything that
reaches it can read and modify every identity in the school. An empty
`identity.public_url` turns identity off, and every route that needs a
session stays unregistered.

On first boot, when no account holds the `admin` role, the server creates one
from `identity.bootstrap` and prints a generated password once. The account
must change it at first sign-in. There is no default password.

## Solving nodes

A solve runs on an Arrango node, reached over gRPC with mutual TLS and no CA:
each side pins the other's self-signed certificate by its SHA-256
fingerprint. This server's keypair is in `arrango.identity_dir`; replacing it
means every node must pair again.

Pairing is one paste. On the node, `arrango-node pair --host <address this
server dials> ...` prints a one-time `arrango://` link; here,
`goptivum-server arrango pair "<link>" --label <name>` (or Backend > Nodes in
the desktop). The token is sent only after the node's certificate has
matched the fingerprint in the link. The link is a secret until it is used.

Each node carries this server's own limits, solves at once and workers per
solve, never above the ceilings the node's operator granted at pairing. A
solve is never dispatched with `num_workers = 0`, which means every core to
the solver at about 0.9 GB of memory each.

## plaprobe

Looks inside a school's `.pla` without printing the school.

```
plaprobe census FILE   every element, its count, and the names of its attributes
plaprobe tree FILE     where each element sits and what type each attribute holds
plaprobe xml FILE      the decoded document, and nothing else, for a redirect
```

`census` and `tree` report shape and never a value, so their output can go in
a bug report. `xml` writes what is inside, which for a real file is a school's
personal data.

```sh
just pla-probe census tests/data/plan.pla
```

## Checks

```sh
just check    # fmt-check, vet, test, check-locales, check-generated
```

## Language

What the server writes for a person to read (its log and its command line)
is localized with S99Locale, in `en` and `pl`. `application.language` picks
it; `--lang` overrides it for one run. Help, version output and early usage
errors use `--lang`, else `GOPTIVUM_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`,
else English; `plaprobe` uses the same environment. Help never loads or
creates a configuration file. Sources are in `i18n/locales/`; `just locales`
regenerates the catalogs.

A response never carries a sentence: `Error.message` is a key such as
`error.notFound`, and the client words it in its own language.

## The contract

`contracts/backend/` is the source of truth for the HTTP API. The bundle and
the generated Go boundary are regenerated from it, never edited.

```sh
just contracts        # lint the authored tree, bundle it to dist/openapi.yaml
just generate         # locales, shared contracts, config types, server boundary, protobuf
just check-generated  # fails when a generated file does not match its source
```

`/api/info` is unversioned and frozen: a client has to learn which version a
server speaks before it can call a versioned route. Its four required fields
are `product` (always `goptivum`), `api_version`, `min_client_version` and
`server_version`; `addresses` and `maximum_upload_bytes` are optional.

## Layout

```
backend/       the server: commands, HTTP API, node policy, core stores, identity, formats, config
document/      nested module: the plan document, its codec and the .gpf container
solveclient/   nested module: the client side of the link to an Arrango node, shared with the desktop
proto/         the document's .proto, generated into document/gen
contracts/     the authored OpenAPI tree and its bundle
i18n/          S99Locale sources
deploy/        systemd user unit; Kratos config and identity schema
scripts/       locale, protobuf, shared-contract and password helpers (Python)
tools/         plaprobe
tests/         fixtures/ and golden/ are synthetic; data/ is real and gitignored
```
