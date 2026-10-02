#!/usr/bin/env python3
# scripts/shared-contracts.py
"""Copy s99ab's shared OpenAPI fragments into contracts/shared/.

Redocly resolves $ref by path and the module cache path carries a version, so
the fragments are copied in rather than referenced where they sit. Copied, not
retyped: the error envelope is the shape every s99ab client is generated
against, and a variation of it breaks them all. This script is what makes the
copy verifiable instead of a promise in a comment.
"""

import argparse
import difflib
from pathlib import Path
import shutil
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / "contracts/shared"

# Each pair is a file in s99ab's contracts tree and where it lands here.
FRAGMENTS = (
    ("contracts/schemas/error.yaml", "schemas/error.yaml"),
    ("contracts/schemas/pagination.yaml", "schemas/pagination.yaml"),
    ("contracts/parameters/pagination.yaml", "parameters/pagination.yaml"),
)

HEADER = (
    "# Copied from s99ab by scripts/shared-contracts.py. Do not edit.\n"
    "# Run `just shared-contracts` after bumping s99ab.\n"
)


def module_dir():
    result = subprocess.run(
        ["go", "list", "-m", "-f", "{{.Dir}}", "github.com/smegg99/s99ab"],
        check=True, capture_output=True, text=True, cwd=ROOT,
    )
    path = Path(result.stdout.strip())
    if not path.is_dir():
        raise RuntimeError(f"{path}: s99ab is not in the module cache")
    return path


def wanted(source):
    return {
        target: HEADER + (source / origin).read_text()
        for origin, target in FRAGMENTS
    }


def write(source):
    for target, text in wanted(source).items():
        path = TARGET / target
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)


def check(source):
    drift = False
    for target, desired in wanted(source).items():
        path = TARGET / target
        actual = path.read_text() if path.exists() else ""
        if actual == desired:
            continue
        drift = True
        sys.stdout.writelines(difflib.unified_diff(
            actual.splitlines(keepends=True), desired.splitlines(keepends=True),
            fromfile=str(Path("contracts/shared") / target), tofile="s99ab",
        ))
    if drift:
        raise RuntimeError("shared contract fragments are stale; run just shared-contracts")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="fail when a copy no longer matches s99ab")
    arguments = parser.parse_args()
    source = module_dir()
    if arguments.check:
        check(source)
    else:
        write(source)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
