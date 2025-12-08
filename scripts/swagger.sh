#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "Regenerating Swagger docs ($ROOT_DIR)"

SWAG_VERSION="v1.16.6" # keep in sync with go.mod
SWAG_BIN="$(go env GOPATH)/bin/swag"

if ! command -v swag >/dev/null 2>&1 && ! command -v "$SWAG_BIN" >/dev/null 2>&1; then
  echo "Installing swag CLI ($SWAG_VERSION)"
  go install "github.com/swaggo/swag/cmd/swag@${SWAG_VERSION}"
fi

if command -v swag >/dev/null 2>&1; then
  SWAG_CMD="swag"
else
  SWAG_CMD="$SWAG_BIN"
fi

OUT_DIR="./docs"
mkdir -p "$OUT_DIR"

echo "Cleaning old docs"
rm -f "$OUT_DIR"/docs.go "$OUT_DIR"/swagger.json "$OUT_DIR"/swagger.yaml || true

echo "Running swag init"
"$SWAG_CMD" init \
  -g ./api/v1/api.go \
  -o "$OUT_DIR" \
  --parseDependency \
  --parseInternal

echo "Swagger docs updated:"
ls -1 "$OUT_DIR"/swagger.* "$OUT_DIR"/docs.go