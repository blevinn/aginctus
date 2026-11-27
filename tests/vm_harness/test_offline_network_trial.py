"""Hostless checks for fixed offline-network probe scripts and markers."""

import importlib.util
import unittest
from pathlib import Path
from unittest.mock import Mock

SPEC = importlib.util.spec_from_file_location(
    "offline_network_trial",
    Path(__file__).resolve().parents[2] / "internal/testing/testenv/nix/offline_network_trial.py",
)
network = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(network)


class OfflineNetworkTest(unittest.TestCase):
    def test_fixed_guest_and_listener_scripts_compile(self):
        for script in (network.NETWORK_SERVER, network.NETWORK_POSITIVE, network.NETWORK_NEGATIVE):
            compile(script, "<fixed-network-probe>", "exec")

    def test_marker_requires_exact_output(self):
        machine = Mock()
        machine.succeed.return_value = "negative\n"
        network._network_marker(machine, "probe", "negative")
        for output in ("positive", "negative\nextra", ""):
            with self.subTest(output=output):
                machine.succeed.return_value = output
                with self.assertRaises(RuntimeError):
                    network._network_marker(machine, "probe", "negative")

    def test_endpoint_counts_are_bounded_numeric(self):
        machine = Mock()
        machine.succeed.side_effect = ["", "4\n"]
        self.assertEqual(network._network_count(machine), 4)
        for output in ("", "4\n5", "-1", "1000000"):
            with self.subTest(output=output):
                machine.succeed.side_effect = ["", output]
                with self.assertRaises(RuntimeError):
                    network._network_count(machine)


if __name__ == "__main__":
    unittest.main()
