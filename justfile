# justfile

bin := "bin/goptivum-server"

default:
	@just --list --unsorted

# --- the three verbs every repo has ---

# Build the production artifact.
build:
	#!/usr/bin/env bash
	set -euo pipefail
	mkdir -p bin
	go build -trimpath \
		-ldflags "-X github.com/smegg99/goptivum/backend/version.Release=$(cat VERSION) -X github.com/smegg99/goptivum/backend/version.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown) -X github.com/smegg99/goptivum/backend/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		-o {{bin}} ./backend

# Run what build produced.
run: build
	./{{bin}} serve

# Development run: no artifact, fastest bringup.
dev:
	go run ./backend serve

# Unit tests. Corpus and integration tests skip when their inputs are absent.
test:
	go test ./...
	cd document && go test ./...
	cd solveclient && go test ./...

# Tests including the real-file corpus, verbose about what it found or skipped.
test-corpus:
	go test ./backend/formats/... -run Corpus -v

# Postgres and MinIO for local work.
infra-up:
	docker compose up -d --wait

# Stops the stack and keeps its data.
infra-down:
	docker compose down --remove-orphans

# Follows the database and object storage logs.
infra-logs:
	docker compose logs -f postgres minio

# Throws away the volumes as well: a fresh database and an empty bucket.
infra-reset:
	docker compose down --volumes --remove-orphans

# Tests the first-sign-in loop against a real Kratos. Point it at a disposable
# one: the test creates and deletes identities in whatever it is given.
test-kratos ADMIN="http://127.0.0.1:14434" PUBLIC="http://127.0.0.1:14433":
	GOPTIVUM_TEST_KRATOS_ADMIN={{ADMIN}} GOPTIVUM_TEST_KRATOS_PUBLIC={{PUBLIC}} \
		go test ./backend/identity/ -run Infra -v -count 1

# Tests against the real Postgres and MinIO that infra-up starts.
test-infra:
	#!/usr/bin/env bash
	set -euo pipefail
	export GOPTIVUM_TEST_POSTGRES_DSN="postgres://goptivum:goptivum-development@127.0.0.1:5434/goptivum?sslmode=disable"
	export GOPTIVUM_TEST_MINIO_ENDPOINT="127.0.0.1:9004"
	export GOPTIVUM_TEST_MINIO_ACCESS_KEY="goptivum"
	export GOPTIVUM_TEST_MINIO_SECRET_KEY="goptivum-development"
	export GOPTIVUM_TEST_MINIO_BUCKET="goptivum-plans"
	go test ./backend/core/... -run Infra -v -count 1

# Copy s99ab's shared OpenAPI fragments into contracts/shared/. The error
# envelope is the shape every s99ab client is generated against, so it is
# sourced rather than retyped.
shared-contracts:
	python3 scripts/shared-contracts.py

# Fails when a shared fragment no longer matches the s99ab in go.mod.
check-shared-contracts:
	python3 scripts/shared-contracts.py --check

# Lint the authored contract tree and bundle it for the generators.
contracts: check-shared-contracts
	./contracts/node_modules/.bin/redocly lint --config contracts/backend/redocly.yaml backend@v1
	./contracts/node_modules/.bin/redocly bundle --config contracts/backend/redocly.yaml backend@v1 --component-renaming-conflicts-severity=error -o contracts/backend/dist/openapi.yaml

# Generate the Go server boundary from the bundled contract.
generate-backend: contracts
	cd backend && go tool oapi-codegen -config api/v1/oapi-codegen.yaml ../contracts/backend/dist/openapi.yaml

# Generate the configuration types from the CUE schema.
generate-config:
	cd backend/common/config && go tool cue exp gengotypes ./...

# Generate the Go protobuf bindings from proto/ into the document module. Needs
# the Arrango checkout beside this one, the same coupling go.mod's replace
# already imposes.
proto-go:
	python3 scripts/proto-go.py

# Fails when the bindings do not match proto/.
proto-check:
	python3 scripts/proto-go.py --check

# Generate the locale catalogs and copy them to the Go consumer.
locales:
	python3 scripts/locales.py

# Fails when a catalog or its copy is stale.
check-locales:
	python3 scripts/locales.py --check

generate: locales shared-contracts generate-config generate-backend proto-go

# Fails when a generated file does not match the authored source it came from.
check-generated:
	python3 scripts/check-generated.py

fmt:
	gofmt -s -w backend tools document solveclient

fmt-check:
	#!/usr/bin/env bash
	set -euo pipefail
	files="$(gofmt -l backend tools document solveclient)"
	if [[ -n "$files" ]]; then printf '%s\n' "$files"; exit 1; fi

vet:
	go vet ./...
	cd document && go vet ./...
	cd solveclient && go vet ./...

tidy:
	go mod tidy

# Solve on a real Arrango Node. Mint the URI on the node itself, over a tunnel:
#   ssh -N -L 19443:127.0.0.1:9443 <user>@<host>
#   arrango-node pair --host 127.0.0.1:19443 --label goptivum-test --ttl 15m
#   just test-arrango "arrango://..."
# A token is one-time, so every run needs a fresh URI.
test-arrango URI:
	GOPTIVUM_TEST_ARRANGO_URI="{{URI}}" go test ./backend/arrango/... -run Infra -v -count 1

# Look inside a real .pla without printing a school into a terminal log.
#   just pla-probe census tests/data/school.pla   elements and attribute names
#   just pla-probe tree tests/data/school.pla     where elements sit, value shapes
pla-probe MODE FILE:
	go run ./tools/plaprobe {{MODE}} "{{FILE}}"

check: fmt-check vet test check-locales check-generated
