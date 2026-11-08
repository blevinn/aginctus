"""Fail-closed applicability selector for expensive PR Nix contract builds.

Only explicitly recognized documentation-only changes can exempt the focused
Nix security contracts. This is not a release-coverage or trust assertion.
"""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
SHA = re.compile(r"[0-9a-f]{40}(?:[0-9a-f]{24})?\Z")
ROOT_DOCS = frozenset({"README.md"})


def is_docs_only_path(path: str) -> bool:
    """Conservative allowlist, not a broad extension-based exemption."""
    return path in ROOT_DOCS or (
        path.startswith("docs/")
        and path.endswith((".md", ".markdown"))
        and ".." not in Path(path).parts
    )


def requires_nix(paths: list[str]) -> bool:
    # Empty/unknown input is never evidence that checks are unnecessary.
    return not paths or any(not is_docs_only_path(path) for path in paths)


def changed_paths(base: str, head: str = "HEAD") -> list[str]:
    if not SHA.fullmatch(base):
        raise ValueError("invalid base revision")
    # --no-renames ensures rename-from paths are not silently discarded.
    result = subprocess.run(
        ["git", "diff", "--name-only", "--no-renames", "-z", base, head],
        cwd=ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        check=True,
    )
    raw = result.stdout
    if not raw.endswith(b"\0"):
        raise ValueError("missing terminated path list")
    names = raw[:-1].split(b"\0")
    return [item.decode("utf-8", errors="strict") for item in names]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True)
    args = parser.parse_args()
    try:
        needed = requires_nix(changed_paths(args.base))
        code = 0
    except (OSError, UnicodeError, ValueError, subprocess.CalledProcessError):
        # An invalid/missing diff does not excuse security checks. The
        # selector itself fails as a second layer of failure evidence.
        needed, code = True, 2
    output = "true" if needed else "false"
    with Path(os.environ["GITHUB_OUTPUT"]).open("a", encoding="utf-8") as stream:
        stream.write(f"nix_required={output}\n")
    print(f"Nix security contracts required: {output}")
    return code


if __name__ == "__main__":
    sys.exit(main())
