# Goptivum Server

The server for [Goptivum Desktop](https://github.com/smegg99/Goptivum-Desktop).
It manages a school's accounts and timetable projects, imports Optivum and
`.gpf` files, keeps saved versions, and sends solves to
[Arrango](https://github.com/smegg99/Arrango) nodes.

Linux only for now.

## Build

Requires Go 1.27, [just](https://github.com/casey/just), and an Arrango checkout
at `../Arrango`.

```sh
just build
```

The binary is `bin/goptivum-server`.

## Run

Accounts require Ory Kratos. For local development, start it with Docker Compose:

```sh
docker compose up -d --wait kratos
just run
```

The server listens on `127.0.0.1:8833`. If that port is taken, use
`bin/goptivum-server serve --addr 127.0.0.1:8834`.

On first run it creates `config.yaml`, `settings.yaml`, and an admin account,
then prints the generated password once. Data defaults to `goptivum.db`
(SQLite) and `documents/`.

## Development

`just test` runs the Go tests. `just check` also checks formatting, vet,
locales, and generated files. Generation checks need Python 3, pnpm,
protoc 33.1 and protoc-gen-go v1.36.11. Install the contract tools with
`pnpm --dir contracts install`.
