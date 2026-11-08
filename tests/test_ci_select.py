"""Negative-first test coverage for CI Nix applicability classification."""

import subprocess
import unittest
from unittest.mock import patch

from scripts.checks import ci_select


class NixSelectorTests(unittest.TestCase):
    def test_documentation_only(self):
        self.assertFalse(ci_select.requires_nix(["docs/README.md", "README.md"]))
        self.assertFalse(ci_select.requires_nix(["docs/internal/design.markdown"]))

    def test_unknown_or_security_paths_run_checks(self):
        for path in (
            ".github/workflows/hermetic.yml",
            "scripts/checks/ci_select.py",
            "nix/nixos-module.nix",
            "flake.nix",
            "flake.lock",
            "internal/config/config.go",
            "internal/cli/config.go",
            "internal/gateway/server.go",
            "go.mod",
            "tests/test_ci_select.py",
            "docs/../internal/config/unsafe.md",
            "docs/README.md/extra",
            "docs/image.png",
        ):
            with self.subTest(path=path):
                self.assertTrue(ci_select.requires_nix([path]))
        self.assertTrue(ci_select.requires_nix([]))
        self.assertTrue(ci_select.requires_nix(["docs/a.md", "nix/b.nix"]))

    def test_invalid_commit_is_not_accepted(self):
        with self.assertRaises(ValueError):
            ci_select.changed_paths("--output=/tmp/foo")

    @patch.object(ci_select.subprocess, "run")
    def test_git_diff_uses_no_renames_and_nul(self, run):
        run.return_value.stdout = b"docs/renamed.md\0internal/config/old.go\0"
        paths = ci_select.changed_paths("a" * 40)
        self.assertEqual(paths, ["docs/renamed.md", "internal/config/old.go"])
        self.assertTrue(ci_select.requires_nix(paths))
        args = run.call_args.args[0]
        self.assertIn("--no-renames", args)
        self.assertIn("-z", args)
        self.assertNotIn("shell", run.call_args.kwargs)

    @patch.object(ci_select.subprocess, "run")
    def test_incomplete_diff_fails_closed(self, run):
        run.return_value.stdout = b"docs/README.md"
        with self.assertRaises(ValueError):
            ci_select.changed_paths("b" * 40)

    @patch.object(ci_select.subprocess, "run")
    def test_failed_diff_raises(self, run):
        run.side_effect = subprocess.CalledProcessError(1, ["git", "diff"])
        with self.assertRaises(subprocess.CalledProcessError):
            ci_select.changed_paths("c" * 40)


if __name__ == "__main__":
    unittest.main()
