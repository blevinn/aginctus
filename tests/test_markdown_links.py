"""End-to-end offline checks in disposable Git repositories, without broken docs."""

from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "scripts/check_markdown_links.py"


class MarkdownLinksTests(unittest.TestCase):
    def scan(self, files, untracked=None):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            for name, text in files.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text, encoding="utf-8")
            subprocess.run(["git", "add", "."], cwd=root, check=True)
            for name, text in (untracked or {}).items():
                (root / name).write_text(text, encoding="utf-8")
            return subprocess.run(
                [sys.executable, str(SCRIPT), "--root", str(root)],
                capture_output=True, text=True, timeout=30,
            )

    def test_commonmark_links_and_heading_collisions(self):
        result = self.scan({
            "AGENTS.md": "[nested](docs/guide.markdown#repeat-1-1)\n",
            "docs/guide.markdown": (
                "# Repeat\n# Repeat\n# Repeat-1\n"
                "Setext *heading* `code`\n---\n"
                "## Café_世界!\n"
                '[one][ REF ] and [ref][] and [ref]\n\n'
                '[ref]: <space name.md#hello-world> "reference title"\n\n'
                '[title](space%20name.md#hello-world "a title")\n'
                '[angle](<space name.md#hello-world> "a title")\n'
                '[parens](nested_(file).md)\n'
                r'[escaped path](nested_\(file\).md)' + '\n'
                '[setext](#setext-heading-code) [unicode](#caf%C3%A9_%E4%B8%96%E7%95%8C)\n'
                '![image](../asset.png) [directory](../docs/)\n'
            ),
            "docs/space name.md": "# Hello *world*\n",
            "docs/nested_(file).md": "# Target\n",
            "asset.png": "not parsed as Markdown",
        })
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("4 files", result.stdout)

    def test_code_escapes_and_external_targets_are_not_checked(self):
        result = self.scan({"README.md": (
            "````md\n# Fake\n[bad](absent.md)\n```\n[still code](no.md)\n````\n"
            "~~~\n[bad][undefined]\n~~~\n"
            "    [indented](absent.md)\n\n"
            "`[inline](absent.md)` and ``[inline](absent.md)``\n"
            r"\[escaped](absent.md)" + "\n"
            "<!-- [comment](absent.md) -->\n"
            "[web](https://example.invalid) [mail](mailto:invalid@example.invalid)\n"
            "[protocol](//example.invalid/no.md)\n"
        )})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("0 local links, 3 external/scheme links unchecked", result.stdout)

    def test_actionable_failures_and_no_document_exclusions(self):
        cases = [
            ("[file](missing.md)", "missing local target"),
            ("[fragment](#absent)", "missing heading fragment #absent"),
            ("![image](missing.png)", "missing local target"),
            ("[reference][id]\n\n[id]: missing.md", "missing local target"),
            ("[reference][id]\n\n[id]: target.md#absent", "missing heading fragment"),
            ("[reference][undefined]", "undefined reference [undefined]"),
            ("[collapsed][]", "undefined reference [collapsed]"),
            ("![image][undefined]", "undefined reference [undefined]"),
            ("[escape](../../outside.md)", "escapes repository"),
            ("[bad](bad%zz.md)", "missing local target"),
            ("[bad](bad%ff.md)", "invalid start byte"),
            ("[nul](bad%00.md)", "NUL"),
            ("[absolute](/etc/passwd)", "absolute local paths"),
            ("[fake](target.md#fake)", "missing heading fragment #fake"),
        ]
        for text, message in cases:
            with self.subTest(text=text):
                result = self.scan({
                    "templates/guide.markdown": "# Guide\n\n" + text + "\n",
                    "templates/target.md": "```\n# Fake\n```\n# Real\n",
                })
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("templates/guide.markdown:3:", result.stderr)
                self.assertIn(message, result.stderr)

    def test_multiline_diagnostics_and_reference_label_normalization(self):
        result = self.scan({"README.md": (
            "# Guide\n\nparagraph\n[missing](absent.md)\n\n"
            "[multi][A\n B]\n\n[a b]: absent.md\n"
        )})
        self.assertEqual(result.returncode, 1)
        self.assertIn("README.md:4:", result.stderr)
        self.assertIn("README.md:6:", result.stderr)
        self.assertNotIn("undefined reference", result.stderr)

    def test_only_tracked_documents_but_untracked_targets_can_exist(self):
        result = self.scan(
            {"README.md": "[target](local.txt)\n"},
            {"untracked.md": "[broken](missing.md)\n", "local.txt": "exists"},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("1 files, 1 local links", result.stdout)

    def test_missing_tracked_document_is_a_failure(self):
        # A tracked symlink with a missing target must not disappear from discovery.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            (root / "missing.md").symlink_to("absent")
            subprocess.run(["git", "add", "."], cwd=root, check=True)
            result = subprocess.run(
                [sys.executable, str(SCRIPT), "--root", str(root)],
                capture_output=True, text=True, timeout=30,
            )
            self.assertEqual(result.returncode, 1)
            self.assertIn("missing.md:1: cannot read Markdown", result.stderr)


if __name__ == "__main__":
    unittest.main()
