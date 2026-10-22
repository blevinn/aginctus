"""Check every Git-tracked Markdown document locally, without network access."""

import argparse
import subprocess
import sys
import unicodedata
from pathlib import Path
from urllib.parse import unquote, urlsplit

from markdown_it import MarkdownIt
from markdown_it.common.utils import normalizeReference
from markdown_it.rules_inline import image, link


def located(rule):
    """Retain source lines while delegating Markdown syntax to markdown-it-py."""
    def parse(state, silent):
        start, count = state.pos, len(state.tokens)
        if not rule(state, silent):
            return False
        if not silent:
            for token in state.tokens[count:]:
                token.meta.setdefault("line", state.src.count("\n", 0, start))
        return True
    return parse


def undefined_reference(state, silent):
    """Lint explicit/collapsed references that CommonMark otherwise renders literally."""
    start = state.pos
    label_start = start + (state.src[start:start + 1] == "!")
    if state.src[label_start:label_start + 1] != "[":
        return False
    end = state.md.helpers.parseLinkLabel(state, label_start, True)
    if end < 0 or state.src[end + 1:end + 2] != "[":
        return False
    ref_end = state.md.helpers.parseLinkLabel(state, end + 1)
    if ref_end < 0:
        return False
    label = state.src[end + 2:ref_end] or state.src[label_start + 1:end]
    if normalizeReference(label) in state.env.get("references", {}):
        return False
    if not silent:
        token = state.push("undefined_reference", "", 0)
        token.content = label
        token.meta["line"] = state.src.count("\n", 0, start)
    state.pos = ref_end + 1
    return True


def parser():
    md = MarkdownIt("commonmark")
    md.inline.ruler.at("link", located(link))
    md.inline.ruler.at("image", located(image))
    md.inline.ruler.after("image", "undefined_reference", undefined_reference)
    return md


def heading_text(tokens):
    return "".join(
        heading_text(token.children) if token.type == "image" else
        token.content if token.type in ("text", "code_inline") else
        " " if token.type in ("softbreak", "hardbreak") else ""
        for token in tokens
    )


def heading_ids(tokens):
    used = set()
    for index, token in enumerate(tokens):
        if token.type != "heading_open":
            continue
        text = heading_text(tokens[index + 1].children).lower()
        base = "".join(
            char for char in text
            if char in "_-" or unicodedata.category(char)[0] not in "PS"
        ).replace(" ", "-")
        slug, suffix = base, 0
        while slug in used:
            suffix += 1
            slug = f"{base}-{suffix}"
        used.add(slug)
    return used


def check(root):
    names = subprocess.run(
        ["git", "ls-files", "-z", "--", "*.md", "*.markdown"],
        cwd=root, check=True, capture_output=True,
    ).stdout.decode().split("\0")
    md, cache = parser(), {}

    def document(path):
        if path not in cache:
            cache[path] = md.parse(path.read_text(encoding="utf-8"))
        return cache[path]

    failures, local, external = [], 0, 0
    for name in filter(None, names):
        source = root / name
        try:
            blocks = document(source)
        except (OSError, UnicodeError) as exc:
            failures.append(f"{name}:1: cannot read Markdown: {exc}")
            continue
        for block in blocks:
            if block.type != "inline":
                continue
            for token in block.children:
                line = block.map[0] + token.meta.get("line", 0) + 1
                if token.type == "undefined_reference":
                    failures.append(f"{name}:{line}: undefined reference [{token.content}]")
                    continue
                target = token.attrGet("href") if token.type == "link_open" else (
                    token.attrGet("src") if token.type == "image" else None
                )
                if target is None:
                    continue
                try:
                    url = urlsplit(target)
                    if url.scheme or url.netloc:
                        external += 1
                        continue
                    local += 1
                    path = unquote(url.path, errors="strict")
                    fragment = unquote(url.fragment, errors="strict")
                    if "\0" in path:
                        raise ValueError("NUL in local target")
                    if path.startswith("/"):
                        raise ValueError("absolute local paths are unsupported; use repository-relative links")
                    resolved = (source.parent / path).resolve() if path else source
                    if not resolved.is_relative_to(root):
                        raise ValueError("local target escapes repository")
                    if not resolved.exists():
                        raise ValueError("missing local target")
                    if fragment:
                        if resolved.suffix.lower() not in (".md", ".markdown"):
                            raise ValueError("heading fragments require a Markdown target")
                        if fragment not in heading_ids(document(resolved)):
                            raise ValueError(f"missing heading fragment #{fragment}")
                except (ValueError, OSError, UnicodeError) as exc:
                    failures.append(f"{name}:{line}: {target!r}: {exc}")
    for failure in failures:
        print(failure, file=sys.stderr)
    print(f"Markdown: {len(list(filter(None, names)))} files, {local} local links, "
          f"{external} external/scheme links unchecked, {len(failures)} failures")
    return bool(failures)


def main():
    args = argparse.ArgumentParser(description=__doc__)
    args.add_argument("--root", type=Path, default=Path.cwd())
    root = args.parse_args().root.resolve()
    try:
        return int(check(root))
    except (OSError, subprocess.CalledProcessError) as exc:
        print(f"Markdown checker: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
