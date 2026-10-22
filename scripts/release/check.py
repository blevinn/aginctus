#!/usr/bin/env python3
"""Validate reviewed release collateral before a tag can produce a draft release."""

import argparse
import re
import sys
from pathlib import Path

TAG_PATTERN = re.compile(
    r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rc\.([1-9][0-9]*))?$"
)


def check_release(root: Path, tag: str) -> str:
    """Return the on-disk version if collateral matches the requested release tag."""
    if not TAG_PATTERN.fullmatch(tag):
        raise ValueError(f"invalid release tag {tag!r}; expected vX.Y.Z[-rc.N]")
    version = tag[1:]

    version_file = (root / "internal/cli/VERSION").read_text(encoding="utf-8")
    if version_file != version + "\n":
        raise ValueError(
            f"internal/cli/VERSION must be {version!r} with one final newline"
        )

    changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
    headings = re.findall(r"^## \[([^\]]+)\]\s*$", changelog, re.MULTILINE)
    if headings.count(version) != 1:
        raise ValueError(f"CHANGELOG.md needs exactly one ## [{version}] section")

    notes = (root / "docs/releases" / f"{tag}.md").read_text(encoding="utf-8")
    if not notes.startswith(f"# Aginctus {tag}\n"):
        raise ValueError(f"release notes need the '# Aginctus {tag}' heading")
    if "## Validation and security evidence" not in notes:
        raise ValueError("release notes must include validation and security evidence")
    if re.search(r"\b(TODO|TBD|PLACEHOLDER)\b", notes, re.IGNORECASE):
        raise ValueError("release notes contain unfinished placeholders")
    return version


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True, help="release tag, e.g. v0.1.0")
    parser.add_argument(
        "--root", type=Path, default=Path(__file__).resolve().parents[2]
    )
    args = parser.parse_args()
    try:
        version = check_release(args.root, args.tag)
    except (OSError, ValueError) as exc:
        print(f"release check failed: {exc}", file=sys.stderr)
        return 1
    print(f"release collateral verified: {args.tag} (version {version})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
