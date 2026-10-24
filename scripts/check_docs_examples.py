"""Check selected operator shell examples against real CLI help, without executing them.

The caller supplies the output of `aginctus help` on stdin. Only lines starting
with `aginctus ` inside shell fences of the canonical intro guides are checked.
Commands in these guides are never run by this checker.
"""

import argparse
import re
import shlex
import sys
from pathlib import Path

from markdown_it import MarkdownIt

GUIDES = ("docs/getting-started.md", "docs/cli.md", "docs/herdr-machine-registration.md")
HELP_LINE = re.compile(r"^\s*(?:Usage:\s*)?aginctus \[global options\] (.+)$")
COMMAND_LINE = re.compile(r"^  ([a-z][^\n]*?)\s{2,}\S")
VARIABLE = re.compile(r"^(?:<[^>]+>|[A-Z][A-Z0-9_-]*|--[\w-]+=[A-Z][\w/-]*)$")


def patterns(help_text):
    """Get command shapes from the CLI's help; no duplicated syntax catalog."""
    result = []
    in_commands = False
    for line in help_text.splitlines():
        if line.strip() == "Commands:":
            in_commands = True
            continue
        if in_commands:
            if not line.strip():
                in_commands = False
            else:
                match = COMMAND_LINE.match(line)
                if match:
                    result.append(match.group(1).split())
        match = HELP_LINE.match(line)
        if match:
            raw = re.split(r"\s{2,}(?:\(|$)", match.group(1), maxsplit=1)[0]
            tokens = raw.split()
            # The top-level generic <command> matches anything, so ignore it.
            if tokens != ["<command>"]:
                result.append(tokens)
    return result


def matches(example, shape):
    """Match help's optional positions/flags and named values, not shell behavior."""
    if not shape:
        return not example
    raw = shape[0]
    optional = raw.startswith("[") and raw.endswith("]")
    token = raw[1:-1] if optional else raw

    def accepts(value):
        if token.startswith("--") and "=" in token:
            key, expected = token.split("=", 1)
            return value.startswith(key + "=") and (
                not VARIABLE.fullmatch(expected) or len(value) > len(key) + 1
            )
        if VARIABLE.fullmatch(token) or (optional and re.fullmatch(r"[a-z_]+", token)):
            return bool(value) and not value.startswith("--")
        return value in token.split("|")

    if optional and matches(example, shape[1:]):
        return True
    return bool(example) and accepts(example[0]) and matches(example[1:], shape[1:])


def examples(markdown):
    md = MarkdownIt("commonmark")
    for block in md.parse(markdown):
        if block.type != "fence" or block.info.strip().split(" ")[0] not in ("sh", "bash", "shell"):
            continue
        for offset, line in enumerate(block.content.splitlines()):
            command = line.strip()
            if not command.startswith("aginctus "):
                continue
            try:
                tokens = shlex.split(command, comments=True)
            except ValueError as exc:
                yield block.map[0] + offset + 2, None, str(exc)
                continue
            if tokens and tokens[0] == "aginctus":
                yield block.map[0] + offset + 2, tokens[1:], None


def check(root, help_text, guides=GUIDES):
    shapes = patterns(help_text)
    if not shapes:
        return ["CLI help contained no recognizable command usage"]
    errors = []
    checked = 0
    for name in guides:
        try:
            content = (root / name).read_text(encoding="utf-8")
        except OSError as exc:
            errors.append(f"{name}: cannot read: {exc}")
            continue
        for line, tokens, error in examples(content):
            checked += 1
            if error or not any(matches(tokens, shape) for shape in shapes):
                errors.append(f"{name}:{line}: unsupported CLI example: "
                              f"{error or 'aginctus ' + ' '.join(tokens)}")
    if checked == 0:
        errors.append("No checked CLI examples found in canonical guides")
    return errors


def main():
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("--root", type=Path, default=Path.cwd())
    args = cli.parse_args()
    failures = check(args.root.resolve(), sys.stdin.read())
    for failure in failures:
        print(failure, file=sys.stderr)
    print(f"Documented CLI examples: {len(failures)} failures")
    return bool(failures)


if __name__ == "__main__":
    sys.exit(main())
