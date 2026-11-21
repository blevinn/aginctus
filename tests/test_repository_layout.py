"""Regression coverage for the root-level ownership guard."""

import unittest

from tools.layout.check import violations


class LayoutContractTests(unittest.TestCase):
    def test_rejects_legacy_top_level_catch_alls(self):
        paths = [
            "scripts/gateway/run.py", "deploy/config.nix", "guest/ssh/helper.sh",
            "test/fixture.json", "assets/icon.svg", "Makefile",
        ]
        self.assertEqual(violations(paths), sorted(paths))

    def test_allows_colocated_and_development_resources(self):
        self.assertEqual(violations([
            "plugins/litellm/runtime/siwc.py",
            "plugins/opencode/assets/config.json",
            "tests/validation/coverage.py",
            "tools/layout/check.py",
            "nix/guest/ssh/host-trust.sh",
            "docs/history/scripts/old-path.md",
            ".just/check.just",
            "justfile",
        ]), [])

    def test_exact_root_names_only(self):
        self.assertEqual(violations([
            "scripts2/example", "guestbook/data", "myMakefile",
            "docs/Makefile", "tests/foo", "script/example",
        ]), [])


if __name__ == "__main__":
    unittest.main()
