#!/usr/bin/env python3
"""Check generated files against current inputs without changing the worktree."""

from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def run(*args, cwd=ROOT):
    subprocess.run(args, cwd=cwd, check=True)


def main():
    run("python3", "scripts/shared-contracts.py", "--check")
    run("python3", "scripts/proto-go.py", "--check")
    with tempfile.TemporaryDirectory(prefix="goptivum-generated-") as directory:
        parent = Path(directory)
        copy = parent / "Goptivum"
        copy.mkdir()
        # Relative module replacements keep their real consumers. No local
        # configuration, database, credentials or Git metadata is copied.
        (parent / "Arrango").symlink_to(ROOT.parent / "Arrango", target_is_directory=True)
        for module in ("document", "solveclient"):
            (copy / module).symlink_to(ROOT / module, target_is_directory=True)
        for name in ("go.mod", "go.sum"):
            shutil.copyfile(ROOT / name, copy / name)
        shutil.copytree(ROOT / "backend/common/config", copy / "backend/common/config")
        shutil.copytree(ROOT / "contracts", copy / "contracts",
                        ignore=shutil.ignore_patterns("node_modules", "dist"))
        bundle = copy / "contracts/backend/dist/openapi.yaml"
        bundle.parent.mkdir()
        redocly = str(ROOT / "contracts/node_modules/.bin/redocly")
        config = str(copy / "contracts/backend/redocly.yaml")
        run(redocly, "lint", "--config", config, "backend@v1", cwd=copy)
        run(redocly, "bundle", "--config", config, "backend@v1",
            "--component-renaming-conflicts-severity=error", "-o", str(bundle), cwd=copy)
        boundary = copy / "api.gen.go"
        boundary_config = copy / "oapi-codegen.yaml"
        boundary_config.write_text((ROOT / "backend/api/v1/oapi-codegen.yaml").read_text().replace(
            "output: api/v1/gen/api.gen.go", f"output: {boundary}"))
        run("go", "tool", "oapi-codegen", "-config", str(boundary_config), str(bundle))
        run("go", "tool", "cue", "exp", "gengotypes", "./...", cwd=copy / "backend/common/config")
        outputs = {
            "contracts/backend/dist/openapi.yaml": bundle,
            "backend/api/v1/gen/api.gen.go": boundary,
            "backend/common/config/cue_types_config_gen.go": copy / "backend/common/config/cue_types_config_gen.go",
        }
        stale = [name for name, fresh in outputs.items()
                 if not (ROOT / name).exists() or (ROOT / name).read_bytes() != fresh.read_bytes()]
        if stale:
            raise SystemExit("Stale generated output, run just generate:\n" + "\n".join(stale))


if __name__ == "__main__":
    main()
