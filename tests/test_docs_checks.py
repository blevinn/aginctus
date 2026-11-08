"""Hermetic, negative-first tests for documentation discoverability and CLI examples."""

import tempfile
import unittest
from pathlib import Path

from markdown_it import MarkdownIt

from scripts import check_docs_examples, check_markdown_links

HELP = """Usage:
  aginctus [command]

Available Commands:
  gateway     Inspect and reconcile gateway
  network     Inspect management network

Subcommand syntax (derived from registered Cobra commands):
  aginctus help [command]
  aginctus config get PATH
  aginctus gateway apply
  aginctus gateway validate
  aginctus network apply
      --dry-run   Show intended change without mutation
      --force     Permit supported replacement
      -h, --help  Show help
  aginctus workload apply [NAME]
      --dry-run   Inspect without mutation
      --force     Permit supported replacement
  aginctus images list  (aliases: ls)
      --project string   Explicit owned local project
  aginctus images import ALIAS PATH
      --project string   Explicit owned local project
      --confirm-create   Confirm create-only import
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
        self.assertEqual(check_markdown_links.check_index(self.root, ["README.md"], MarkdownIt("commonmark")), [])

    def test_new_public_doc_needs_index_entry(self):
        self.write("docs/README.md", "# Docs\n[Guide](guide.md)\n")
        self.write("docs/guide.md", "# Guide\n")
        self.write("docs/new-operator.md", "# New operator\n")
        names = ["docs/README.md", "docs/guide.md", "docs/new-operator.md"]
        failures = check_markdown_links.check_index(self.root, names, MarkdownIt("commonmark"))
        self.assertEqual(len(failures), 1)
        self.assertIn("docs/new-operator.md", failures[0])
        self.write("docs/README.md", "# Docs\n[Guide](guide.md)\n[New](new-operator.md)\n")
        self.assertEqual(check_markdown_links.check_index(self.root, names, MarkdownIt("commonmark")), [])

    def test_design_adr_and_historical_notes_exempt(self):
        self.write("docs/README.md", "# Docs\n[Guide](guide.md)\n")
        self.write("docs/guide.md", "# Guide\n")
        names = ["docs/README.md", "docs/guide.md"]
        for suffix in ("design/proposal.md", "adr/0003.md", "milestones/old.md", "releases/TEMPLATE.md", "internal/note.md"):
            names.append("docs/" + suffix)
        self.assertEqual(check_markdown_links.check_index(self.root, names, MarkdownIt("commonmark")), [])

    def test_nested_active_docs_must_be_indexed(self):
        self.write("docs/README.md", "# Docs\n")
        names = ["docs/README.md", "docs/workloads/example.md"]
        failures = check_markdown_links.check_index(self.root, names, MarkdownIt("commonmark"))
        self.assertEqual(len(failures), 1)
        self.assertIn("docs/workloads/example.md", failures[0])

    def test_section_indexes_can_discover_nested_active_pages(self):
        self.write("docs/README.md", "# Docs\n[Operations](operations/README.md)\n")
        self.write("docs/operations/README.md", "# Operations\n[Host](host/README.md)\n")
        self.write("docs/operations/host/README.md", "# Host\n[Audit](audit.md)\n")
        self.write("docs/operations/host/audit.md", "# Read-only audit\n")
        names = [
            "docs/README.md", "docs/operations/README.md",
            "docs/operations/host/README.md", "docs/operations/host/audit.md",
        ]
        self.assertEqual(
            check_markdown_links.check_index(self.root, names, MarkdownIt("commonmark")),
            [],
        )

    def test_transitive_index_rejects_orphans_even_with_a_cycle(self):
        self.write("docs/README.md", "# Docs\n[Ops](operations/README.md)\n")
        self.write("docs/operations/README.md", "# Ops\n[Home](../README.md)\n")
        self.write("docs/operations/host/unlinked.md", "# Orphan\n")
        names = [
            "docs/README.md", "docs/operations/README.md",
            "docs/operations/host/unlinked.md",
        ]
        failures = check_markdown_links.check_index(self.root, names, MarkdownIt("commonmark"))
        self.assertEqual(len(failures), 1)
        self.assertIn("docs/operations/host/unlinked.md", failures[0])

    def test_historical_pages_exempt_from_active_index(self):
        self.write("docs/README.md", "# Docs\n")
        self.write("docs/history/old-pilot.md", "# Historical evidence\n")
        names = ["docs/README.md", "docs/history/old-pilot.md"]
        self.assertEqual(
            check_markdown_links.check_index(self.root, names, MarkdownIt("commonmark")),
            [],
        )

    def test_cli_help_shapes_prevent_unsafe_examples(self):
        shapes = check_docs_examples.patterns(HELP)

        def accepted(command):
            return any(check_docs_examples.matches(command.split(), shape) for shape in shapes)

        self.assertTrue(accepted("help"))
        self.assertTrue(accepted("config get incus.management.network.name"))
        self.assertTrue(accepted("workload apply dev --dry-run"))
        self.assertTrue(accepted("network apply --dry-run"))
        self.assertTrue(accepted("network apply -h"))
        self.assertTrue(accepted("network apply --help"))
        self.assertFalse(accepted("gateway apply --dry-run"))
        self.assertFalse(accepted("workload apply dev --unsafe"))
        self.assertFalse(accepted("this-command-does-not-exist"))
        self.assertTrue(accepted("images ls --project=aginctus"))
        self.assertTrue(accepted("images import --project aginctus target /tmp/build --confirm-create"))
        self.assertFalse(accepted("images list --force"))
        self.assertFalse(accepted("images import --project aginctus target /tmp/build --force"))
        self.assertFalse(accepted("network apply --confirm-bridge-only"))

    def test_shell_fences_checked_without_execution(self):
        self.write("docs/cli.md", "# CLI\n```sh\naginctus gateway validate\naginctus gateway apply --dry-run\n```\n")
        self.write("docs/getting-started.md", "# Start\n```bash\naginctus network apply --dry-run\n```\n")
        failures = check_docs_examples.check(self.root, HELP, guides=("docs/cli.md", "docs/getting-started.md"))
        self.assertEqual(len(failures), 1)
        self.assertIn("gateway apply --dry-run", failures[0])
        self.write("docs/cli.md", "# CLI\n```sh\naginctus gateway validate\n```\n")
        self.assertEqual(check_docs_examples.check(self.root, HELP, guides=("docs/cli.md", "docs/getting-started.md")), [])


if __name__ == "__main__":
    unittest.main()
