# scripts/proto-go.py
"""Generate Go protobuf bindings with the tool versions recorded in the output.

The envelope imports arrango/v1, so regenerating needs the Arrango checkout
beside this one. The build does not: the bindings are in the document module.
"""

import argparse
import difflib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[1]
MODULE = "github.com/smegg99/goptivum/document"
# The document module, where every goptivum/v1 go_package points.
TARGET = ROOT / "document"
# Arrango's proto tree, resolved the same way go.mod resolves its replace.
ARRANGO = Path(os.environ.get("GOPTIVUM_ARRANGO_DIR", ROOT.parent / "Arrango"))


def checked_tool(path, expected):
    result = subprocess.run(
        [str(path), "--version"], check=True, capture_output=True, text=True
    )
    actual = result.stdout.strip()
    if actual != expected:
        raise RuntimeError(f"{path}: expected {expected!r}, found {actual!r}")
    return str(path)


def tools():
    prefix = Path(os.environ.get("GOPTIVUM_PROTOC_PREFIX", Path.home() / ".local/opt/or-tools"))
    go_bin = Path.home() / "go/bin"
    return {
        "protoc": checked_tool(prefix / "bin/protoc", "libprotoc 33.1"),
        "go": checked_tool(
            shutil.which("protoc-gen-go") or go_bin / "protoc-gen-go",
            "protoc-gen-go v1.36.11",
        ),
    }


def generate(output, generators):
    schemas = sorted((ROOT / "proto/goptivum/v1").glob("*.proto"))
    if not (ARRANGO / "proto/arrango/v1").is_dir():
        raise RuntimeError(f"{ARRANGO}: no Arrango proto tree, set GOPTIVUM_ARRANGO_DIR")
    subprocess.run(
        [
            generators["protoc"],
            "-I", str(ROOT / "proto"), "-I", str(ARRANGO / "proto"),
            f"--plugin=protoc-gen-go={generators['go']}",
            f"--go_out={output}", f"--go_opt=module={MODULE}",
            *map(str, schemas),
        ],
        check=True,
    )


def write_generated(generators):
    """Regenerate document/gen and delete the stubs whose schema moved or went away."""
    with tempfile.TemporaryDirectory(prefix="goptivum-proto-") as temporary:
        generated = Path(temporary)
        generate(generated, generators)
        fresh = {path.relative_to(generated) for path in generated.rglob("*.pb.go")}
        for stale in list((TARGET / "gen").rglob("*.pb.go")):
            if stale.relative_to(TARGET) not in fresh:
                stale.unlink()
        for relative in fresh:
            destination = TARGET / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(generated / relative, destination)


def check_generated(generators):
    with tempfile.TemporaryDirectory(prefix="goptivum-proto-") as temporary:
        generated = Path(temporary)
        generate(generated, generators)
        generated_files = {path.relative_to(generated) for path in generated.rglob("*.pb.go")}
        tracked_files = {path.relative_to(TARGET) for path in (TARGET / "gen").rglob("*.pb.go")}
        drift = False
        for relative in sorted(generated_files | tracked_files):
            actual = (TARGET / relative).read_text() if relative in tracked_files else ""
            desired = (generated / relative).read_text() if relative in generated_files else ""
            if actual == desired:
                continue
            drift = True
            sys.stdout.writelines(difflib.unified_diff(
                actual.splitlines(keepends=True), desired.splitlines(keepends=True),
                fromfile=str(Path("document") / relative),
                tofile=str(Path("regenerated") / relative),
            ))
        if drift:
            raise RuntimeError("Go protobuf bindings are stale; run just proto-go")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--check-tools", action="store_true")
    arguments = parser.parse_args()
    generators = tools()
    if arguments.check:
        check_generated(generators)
    elif not arguments.check_tools:
        write_generated(generators)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
