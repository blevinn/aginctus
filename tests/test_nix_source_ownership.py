"""Offline ownership and import-path guardrails for the public Nix flake.

The Nix evaluator remains authoritative for expression semantics. These tests
only catch repository moves that leave stale flake references or reintroduce
test-only definitions under production nix/.
"""

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FLAKE = ROOT / "flake.nix"


class NixOwnershipTests(unittest.TestCase):
    def test_flake_local_paths_exist(self):
        text = FLAKE.read_text(encoding="utf-8")
        # Local Nix imports, file reads and interpolations in the public flake.
        references = set(
            re.findall(r"\bimport\s+(\./[A-Za-z0-9_./-]+)", text)
            + re.findall(r"\breadFile\s+(\./[A-Za-z0-9_./-]+)", text)
            + re.findall(r"\$\{(\./[A-Za-z0-9_./-]+)\}", text)
        )
        self.assertTrue(references, "expected local flake references")
        for reference in sorted(references):
            with self.subTest(reference=reference):
                self.assertTrue((ROOT / reference).exists(), reference)

    def test_test_owned_nix_files_stay_out_of_production_tree(self):
        retired = (
            "nix/vm-harness",
            "nix/testenv",
            "nix/herdr-socket-vm-test.nix",
            "nix/module-check.nix",
            "nix/home-manager-module-check.nix",
            "nix/firewall-iptables-test.nix",
            "nix/firewall-firewalld-test.nix",
            "nix/firewall-egress-bootstrap-test.nix",
            "nix/firewall-nixos-nft-test.nix",
        )
        for path in retired:
            with self.subTest(path=path):
                self.assertFalse((ROOT / path).exists(), path)

    def test_owned_definitions_and_public_names_remain(self):
        required_paths = (
            "nix/modules/nixos-module.nix",
            "nix/modules/home-manager-module.nix",
            "nix/packages/go.nix",
            "nix/workloads/opencode-image.nix",
            "nix/workloads/oauth-gateway.nix",
            "tests/nix/herdr-socket-vm-test.nix",
            "tests/nix/vm-harness/guest.nix",
            "tests/nix/vm-harness/aginctus-host.nix",
            "internal/testing/testenv/nix/incus-vm-image.nix",
        )
        for path in required_paths:
            with self.subTest(path=path):
                self.assertTrue((ROOT / path).is_file(), path)

        flake = FLAKE.read_text(encoding="utf-8")
        for output in (
            "nixosModules", "homeManagerModules", "packages",
            "vmTests", "checks", "devShells", "formatter",
        ):
            with self.subTest(output=output):
                self.assertRegex(flake, rf"\b{output}\s*=")

        # These names have compatibility significance even though some
        # derivations remain intentionally opt-in and expensive to evaluate.
        for output in (
            "aginctusTestenvRecovery", "aginctusTestenvWorker",
            "herdr-socket-integration", "vm-harness-smoke",
            "accepted-state-incus",
            "testenv-incus-vm-image",
        ):
            with self.subTest(output=output):
                self.assertIn(output, flake)

        # Heavy VM outputs must not accidentally enter routine checks.
        checks = flake.split("      checks = forAllSystems (", 1)[1].split(
            "      devShells = forAllSystems (", 1
        )[0]
        self.assertNotIn("vm-harness-", checks)
        self.assertNotIn("vmTests.", checks)


if __name__ == "__main__":
    unittest.main()
