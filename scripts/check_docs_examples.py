"""Validate operator shell examples against Cobra-generated help, never executing them.

The caller supplies `aginctus help` on stdin. That output includes a route
inventory derived from registered Cobra commands and local flag metadata,
rather than an independent hardcoded command/option table.
"""

import argparse
import re
import shlex
import sys
from dataclasses import dataclass, field
from pathlib import Path

from markdown_it import MarkdownIt

GUIDES = ("docs/getting-started.md", "docs/cli.md", "docs/herdr-machine-registration.md")
GENERATED_MARKER = "Subcommand syntax (derived from registered Cobra commands):"
GENERATED_ROUTE = re.compile(r"^  aginctus (.+)$")
LEGACY_LINE = re.compile(r"^\s*(?:Usage:\s*)?aginctus \[global options\] (.+)$")
LEGACY_COMMAND = re.compile(r"^  ([a-z][^\n]*?)\s{2,}\S")
VARIABLE = re.compile(r"^(?:<[^>]+>|[A-Z][A-Z0-9_-]*|--[\w-]+=[A-Z][\w/-]*)$")


@dataclass
class CommandShape:
    """One CLI path, its allowed local flags, and optional alias spellings."""

    tokens: tuple[str, ...]
    flags: dict[str, bool] = field(default_factory=dict)


def _legacy_shape(tokens):
    positional, flags = [], {}
    for token in tokens:
        if token.startswith("[--") and token.endswith("]"):
            name, sep, _ = token[1:-1].partition("=")
            flags[name] = bool(sep)
        else:
            positional.append(token)
    return CommandShape(tuple(positional), flags)


def _flag_usage(line):
    """Read the flag name/type from the standard pflag FlagUsages format."""
    head = re.split(r"\s{2,}", line.strip(), maxsplit=1)[0]
    tokens = head.replace(",", " ").split()
    names = [token for token in tokens if token.startswith("-")]
    if not names:
        return None
    value = any(not token.startswith("-") for token in tokens)
    return names, value


def patterns(help_text):
    """Extract only registered CLI routes and their flags, never operations."""
    result = []
    in_generated = False
    in_legacy_commands = False
    current = None
    for line in help_text.splitlines():
        if line.strip() == GENERATED_MARKER:
            in_generated, in_legacy_commands, current = True, False, None
            continue

        if in_generated:
            match = GENERATED_ROUTE.match(line)
            if match:
                raw = match.group(1).split(" — ", 1)[0]
                aliases = re.search(r"\(aliases: ([^)]+)\)", raw)
                raw = raw.split("  (aliases:", 1)[0].strip()
                tokens = tuple(raw.split())
                if not tokens:
                    current = None
                    continue
                current = CommandShape(tokens)
                result.append(current)
                # Alias variants share the exact same local flag metadata.
                if aliases:
                    for alias in aliases.group(1).split(","):
                        alias = alias.strip()
                        variant = list(tokens)
                        commands = [
                            index for index, part in enumerate(variant)
                            if re.fullmatch(r"[a-z][a-z0-9-]*", part)
                        ]
                        if commands and alias:
                            variant[commands[-1]] = alias
                            result.append(CommandShape(tuple(variant), current.flags))
                continue
            if current is not None and line.startswith("      "):
                parsed = _flag_usage(line)
                if parsed:
                    names, takes_value = parsed
                    for name in names:
                        current.flags[name] = takes_value
                continue

        # Accept older help fixtures for tests and branch-local compatibility.
        if line.strip() == "Commands:":
            in_legacy_commands = True
            continue
        if in_legacy_commands:
            if not line.strip():
                in_legacy_commands = False
            else:
                legacy = LEGACY_COMMAND.match(line)
                if legacy:
                    result.append(_legacy_shape(legacy.group(1).split()))
        legacy = LEGACY_LINE.match(line)
        if legacy:
            raw = re.split(r"\s{2,}(?:\(|$)", legacy.group(1), maxsplit=1)[0]
            tokens = raw.split()
            if tokens != ["<command>"]:
                result.append(_legacy_shape(tokens))
    return result


def _matches_positions(example, shape):
    if not shape:
        return not example
    raw = shape[0]
    optional = raw.startswith("[") and raw.endswith("]")
    token = raw[1:-1] if optional else raw

    def accepts(value):
        if VARIABLE.fullmatch(token) or (
            optional and re.fullmatch(r"[a-z_][a-z0-9_-]*", token)
        ):
            return bool(value) and not value.startswith("--")
        return value in token.split("|")

    if optional and _matches_positions(example, shape[1:]):
        return True
    return bool(example) and accepts(example[0]) and _matches_positions(example[1:], shape[1:])


def matches(example, shape):
    """Validate the declared flags in any order, then match positionals."""
    positional = []
    index = 0
    while index < len(example):
        value = example[index]
        if not value.startswith("-") or value == "-":
            positional.append(value)
            index += 1
            continue
        flag, sep, argument = value.partition("=")
        takes_value = shape.flags.get(flag)
        if takes_value is None:
            return False
        if takes_value:
            if sep:
                if not argument:
                    return False
            else:
                index += 1
                if index >= len(example) or example[index].startswith("--"):
                    return False
        elif sep and argument not in ("true", "false"):
            return False
        index += 1
    return _matches_positions(positional, shape.tokens)


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
