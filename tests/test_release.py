"""Hermetic, negative-first release collateral and tag checks."""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/release/check.py"


class ReleaseCheckTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / "internal/cli").mkdir(parents=True)
        (self.root / "docs/releases").mkdir(parents=True)
        self.set_version("0.1.0")
        self.write("CHANGELOG.md", "## [Unreleased]\n\n## [0.1.0]\n\n- shipped\n")
        self.write(
            "docs/releases/v0.1.0.md",
            "# Aginctus v0.1.0\n\n## Highlights\n\n- Shipped\n\n"
            "## Validation and security evidence\n\n- Reviewed separately\n",
        )

    def write(self, path, content):
        (self.root / path).write_text(content, encoding="utf-8")

    def set_version(self, version):
        self.write("internal/cli/VERSION", version + "\n")

    def check(self, tag="v0.1.0"):
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--root", str(self.root), "--tag", tag],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_matching_version_changelog_and_notes(self):
        self.assertEqual(self.check().returncode, 0)

    def test_release_candidate_is_supported(self):
        self.set_version("0.1.0-rc.2")
        self.write("CHANGELOG.md", "## [0.1.0-rc.2]\n")
        self.write(
            "docs/releases/v0.1.0-rc.2.md",
            "# Aginctus v0.1.0-rc.2\n\n"
            "## Validation and security evidence\n\n- Recorded\n",
        )
        self.assertEqual(self.check("v0.1.0-rc.2").returncode, 0)

    def test_reject_malformed_tags_before_path_lookup(self):
        for tag in (
            "../v0.1.0",
            "v01.2.3",
            "v0.1.0-rc.0",
            "v0.1.0+meta",
            "v0.1.0-foo",
            "v0.1.0/../../secret",
            "v0.1.0\n",
        ):
            with self.subTest(tag=tag):
                self.assertIn("invalid release tag", self.check(tag).stderr)

    def test_reject_wrong_version_and_extra_newlines(self):
        self.set_version("0.0.0-dev")
        self.assertIn("internal/cli/VERSION", self.check().stderr)
        self.write("internal/cli/VERSION", "0.1.0\n\n")
        self.assertIn("internal/cli/VERSION", self.check().stderr)

    def test_reject_missing_or_duplicate_changelog_version(self):
        self.write("CHANGELOG.md", "## [Unreleased]\n")
        self.assertIn("exactly one", self.check().stderr)
        self.write("CHANGELOG.md", "## [0.1.0]\n## [0.1.0]\n")
        self.assertIn("exactly one", self.check().stderr)

    def test_reject_missing_or_incomplete_notes(self):
        (self.root / "docs/releases/v0.1.0.md").unlink()
        self.assertNotEqual(self.check().returncode, 0)
        self.write(
            "docs/releases/v0.1.0.md",
            "# Aginctus v0.1.0\n## Validation and security evidence\n- TODO\n",
        )
        self.assertIn("placeholders", self.check().stderr)
        self.write("docs/releases/v0.1.0.md", "# Aginctus v0.1.0\n")
        self.assertIn("validation and security", self.check().stderr)

    def test_source_tree_unchanged(self):
        before = sorted(p.relative_to(self.root) for p in self.root.rglob("*"))
        self.check()
        after = sorted(p.relative_to(self.root) for p in self.root.rglob("*"))
        self.assertEqual(before, after)


if __name__ == "__main__":
    unittest.main()
