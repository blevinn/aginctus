"""Hermetic, negative-first tests for documentation discoverability and CLI examples."""

import sys
import tempfile
import unittest
from pathlib import Path

from markdown_it import MarkdownIt

SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"
sys.path.insert(0, str(SCRIPTS))

from check_docs_examples import check as check_examples, matches, patterns  # noqa: E402
from check_markdown_links import check_index  # noqa: E402


HELP = """Usage: aginctus [global options] <command>

Commands:
  help [command]    Display command help
  config get PATH  Read configuration

Usage: aginctus [global options] gateway apply
       aginctus [global options] gateway validate
       aginctus [global options] network apply [--dry-run] [--force]
       aginctus [global options] workload apply [name] [--dry-run] [--force]
"""


class DocsChecksTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        (self.root / "docs").mkdir()

    def write(self, path, text):
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding="utf-8")

    def test_no_docs_tree_requires_no_index(self):
        self.write("README.md", "# Standalone Markdown fixture\n")
        self.assertEqual(check_index(self.root, ["README.md"], MarkdownIt("commonmark")), [])

    def test_new_public_doc_needs_index_entry(self):
        self.write("docs/README.md", "# Docs\n[Guide](guide.md)\n")
        self.write("docs/guide.md", "# Guide\n")
        self.write("docs/new-operator.md", "# New operator\n")
        names = ["docs/README.md", "docs/guide.md", "docs/new-operator.md"]
        failures = check_index(self.root, names, MarkdownIt("commonmark"))
        self.assertEqual(len(failures), 1)
        self.assertIn("docs/new-operator.md", failures[0])
        self.write("docs/README.md", "# Docs\n[Guide](guide.md)\n[New](new-operator.md)\n")
        self.assertEqual(check_index(self.root, names, MarkdownIt("commonmark")), [])

    def test_design_adr_and_historical_notes_exempt(self):
        self.write("docs/README.md", "# Docs\n[Guide](guide.md)\n")
        self.write("docs/guide.md", "# Guide\n")
        names = ["docs/README.md", "docs/guide.md"]
        for suffix in ("design/proposal.md", "adr/0003.md", "milestones/old.md", "releases/TEMPLATE.md", "internal/note.md"):
            names.append("docs/" + suffix)
        self.assertEqual(check_index(self.root, names, MarkdownIt("commonmark")), [])

    def test_nested_active_docs_must_be_indexed(self):
        self.write("docs/README.md", "# Docs\n")
        names = ["docs/README.md", "docs/workloads/example.md"]
        failures = check_index(self.root, names, MarkdownIt("commonmark"))
        self.assertEqual(len(failures), 1)
        self.assertIn("docs/workloads/example.md", failures[0])

    def test_cli_help_shapes_prevent_unsafe_examples(self):
        shapes = patterns(HELP)

        def accepted(command):
            return any(matches(command.split(), shape) for shape in shapes)

        self.assertTrue(accepted("help"))
        self.assertTrue(accepted("config get incus.management.network.name"))
        self.assertTrue(accepted("workload apply dev --dry-run"))
        self.assertTrue(accepted("network apply --dry-run"))
        self.assertFalse(accepted("gateway apply --dry-run"))
        self.assertFalse(accepted("workload apply dev --unsafe"))
        self.assertFalse(accepted("this-command-does-not-exist"))

    def test_shell_fences_checked_without_execution(self):
        self.write("docs/cli.md", "# CLI\n```sh\naginctus gateway validate\naginctus gateway apply --dry-run\n```\n")
        self.write("docs/getting-started.md", "# Start\n```bash\naginctus network apply --dry-run\n```\n")
        failures = check_examples(self.root, HELP, guides=("docs/cli.md", "docs/getting-started.md"))
        self.assertEqual(len(failures), 1)
        self.assertIn("gateway apply --dry-run", failures[0])
        self.write("docs/cli.md", "# CLI\n```sh\naginctus gateway validate\n```\n")
        self.assertEqual(check_examples(self.root, HELP, guides=("docs/cli.md", "docs/getting-started.md")), [])


if __name__ == "__main__":
    unittest.main()
