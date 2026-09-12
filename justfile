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

# Lint the authored contract tree and bundle it for the generators.
contracts:
	./contracts/node_modules/.bin/redocly lint --config contracts/backend/redocly.yaml backend@v1
	./contracts/node_modules/.bin/redocly bundle --config contracts/backend/redocly.yaml backend@v1 --component-renaming-conflicts-severity=error -o contracts/backend/dist/openapi.yaml

# Generate the Go server boundary from the bundled contract.
generate-backend: contracts
	cd backend && go tool oapi-codegen -config api/v1/oapi-codegen.yaml ../contracts/backend/dist/openapi.yaml

# Generate the configuration types from the CUE schema.
generate-config:
	cd backend/common/config && go tool cue exp gengotypes ./...

# Generate the locale catalogs and copy them to the Go consumer.
locales:
	python3 scripts/locales.py

# Fails when a catalog or its copy is stale.
check-locales:
	python3 scripts/locales.py --check

generate: locales generate-config generate-backend

# Fails when a generated file does not match the authored source it came from.
check-generated: generate
	git diff --exit-code -- contracts/backend/dist/openapi.yaml backend/api/v1/gen backend/common/config/cue_types_config_gen.go

fmt:
	gofmt -s -w backend tools

fmt-check:
	#!/usr/bin/env bash
	set -euo pipefail
	files="$(gofmt -l backend tools)"
	if [[ -n "$files" ]]; then printf '%s\n' "$files"; exit 1; fi

vet:
	go vet ./...

tidy:
	go mod tidy

# Look inside a real .pla without printing a school into a terminal log.
#   just pla-probe census tests/data/school.pla   elements and attribute names
#   just pla-probe tree tests/data/school.pla     where elements sit, value shapes
pla-probe MODE FILE:
	go run ./tools/plaprobe {{MODE}} "{{FILE}}"

check: fmt-check vet test check-locales check-generated
