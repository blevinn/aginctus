"""Hostless tests for the bounded runtime resource evidence projection."""

import importlib.util
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location(
    "runtime_resource_trial",
    Path(__file__).resolve().parents[2] / "internal/testing/testenv/nix/runtime_resource_trial.py",
)
resource = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(resource)


class RuntimeResourceTest(unittest.TestCase):
    def test_expected_capacity(self):
        result = resource.validate_resource_observations("2\n", "4000000\n", "34359738368", "512", "0")
        self.assertEqual(
            {item["id"]: item["status"] for item in result},
            {
                "guest_cpu_count": "pass",
                "guest_memory_capacity": "pass",
                "guest_root_disk_capacity": "pass",
            },
        )

    def test_rejects_malformed_or_wrong_capacity(self):
        baseline = ("2", "4000000", "34359738368", "512", "0")
        for index, invalid in (
            (0, "1"), (0, "4"), (0, "2\n3"),
            (1, "2097152"), (1, "4194305"),
            (2, "34359738367"), (2, "34359738880"),
            (3, "0"), (4, "512"),
            (4, "error"),
        ):
            with self.subTest(index=index, invalid=invalid):
                values = list(baseline)
                values[index] = invalid
                with self.assertRaises(RuntimeError):
                    resource.validate_resource_observations(*values)


if __name__ == "__main__":
    unittest.main()
