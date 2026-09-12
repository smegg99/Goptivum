# tests

    data/       real files. GITIGNORED. Never committed.
    fixtures/   small synthetic or anonymised inputs. Committed.
    golden/     expected outputs for fixtures. Committed.

## Why data/ is not committed

A real Optivum export, a real school database dump or a real plan file contains
teacher and student names. That is personal data under GDPR, and a git history
is forever — a file deleted in a later commit is still in the history, still on
every clone, still on GitHub.

So: drop real files into `data/`. They stay on this machine. Tests that need
them **skip** rather than fail when the directory is empty, so a clean checkout
still runs green.

When a real file exposes a bug, reduce it to the smallest input that still
reproduces, strip every real name, and commit that to `fixtures/` with a golden
output. The bug then stays fixed without the school's data ever entering the
repository.
