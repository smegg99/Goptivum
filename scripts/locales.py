#!/usr/bin/env python3
# scripts/locales.py
"""Generate the S99Locale catalogs and copy them to the Go consumer.

go:embed cannot reach outside its own package, so the generated bundles and
accessors are copied into backend/common/messages rather than embedded where
loc writes them.
"""

import argparse
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / "backend/common/messages"

# Each pair is where loc writes and where the Go package reads.
COPIES = (
    (ROOT / "i18n/generated/go", TARGET, "*_gen.go"),
    (ROOT / "i18n/generated/backend", TARGET / "locales", "*.json"),
)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="fail when the catalogs or the copies are stale")
    args = parser.parse_args()

    subprocess.run(
        ["go", "tool", "loc", "check" if args.check else "build", "-c", "i18n/config.cue"],
        cwd=ROOT, check=True,
    )

    stale = []
    for source, destination, pattern in COPIES:
        expected = {file.name: file.read_bytes() for file in source.glob(pattern)}
        actual = {file.name: file.read_bytes() for file in destination.glob(pattern)}
        for name in sorted(expected.keys() | actual.keys()):
            if expected.get(name) == actual.get(name):
                continue
            output = destination / name
            if args.check:
                stale.append(str(output.relative_to(ROOT)))
            elif name in expected:
                output.parent.mkdir(parents=True, exist_ok=True)
                output.write_bytes(expected[name])
            else:
                output.unlink()

    if stale:
        raise SystemExit("Stale locale consumers, run just locales:\n" + "\n".join(stale))


if __name__ == "__main__":
    main()
