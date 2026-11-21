"""Verify the tracked top-level layout; never execute tracked source files."""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

# Root-level catch-alls retired by #876; nested domain-owned names are allowed.
RETIRED_ROOTS = frozenset({"assets", "deploy", "guest", "scripts", "test"})
RETIRED_FILES = frozenset({"Makefile"})


def violations(paths: list[str]) -> list[str]:
    rejected = []
    for name in paths:
        root, _, _ = name.partition("/")
        if root in RETIRED_ROOTS and "/" in name or name in RETIRED_FILES:
            rejected.append(name)
    return sorted(set(rejected))


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    try:
        tracked = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z", "--cached"],
            check=True, capture_output=True,
        ).stdout
    except (OSError, subprocess.CalledProcessError):
        print("layout: unable to enumerate tracked files", file=sys.stderr)
        return 2
    names = [os.fsdecode(path) for path in tracked.split(b"\0") if path]
    rejected = violations(names)
    for name in rejected:
        print(f"layout: retired root path: {name}", file=sys.stderr)
    if rejected:
        return 1
    print("layout: tracked root directory contract passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
