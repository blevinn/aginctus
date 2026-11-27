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
    def test_cpu_hotplug_topology(self):
        self.assertEqual(resource.validate_cpu_topology("0-1", "0-1", "0-3"), "2")
        self.assertEqual(resource.validate_cpu_topology("0,2", "0,2", "0-3"), "2")
        for topology in (
            ("0-3", "0-3", "0-3"),
            ("0-1", "0-3", "0-3"),
            ("0-1", "0-1", "0"),
            ("0", "0-1", "0-3"),
            ("0,0", "0-1", "0-3"),
            ("0-4096", "0-1", "0-3"),
            ("1-0", "0-1", "0-3"),
            ("bad", "0-1", "0-3"),
        ):
            with self.subTest(topology=topology):
                with self.assertRaises(RuntimeError):
                    resource.validate_cpu_topology(*topology)

    def test_cleanup_allows_only_pinned_image_cache(self):
        fingerprint = "a" * 64
        cache = ("image", fingerprint)
        states = [("foreign-sentinel", "Stopped")]
        disks = [("virtual-machine", "foreign-sentinel")]
        configs = [("foreign-sentinel", "unchanged")]
        baseline = states, disks, configs
        resource.validate_resource_cleanup(baseline, baseline, fingerprint)
        with_cache = states, sorted([*disks, cache]), configs
        resource.validate_resource_cleanup(baseline, with_cache, fingerprint)
        resource.validate_resource_cleanup(with_cache, with_cache, fingerprint)
        for current in (
            (states, sorted([*disks, ("image", "b" * 64)]), configs),
            (states, sorted([*disks, cache, cache]), configs),
            (states, sorted([*disks, ("virtual-machine", "resource-probe")]), configs),
            (states, sorted([*disks, ("custom", "foreign-volume")]), configs),
            (states, [cache], configs),
            ([], disks, []),
            (states, disks, [("foreign-sentinel", "changed")]),
        ):
            with self.subTest(current=current):
                with self.assertRaises(RuntimeError):
                    resource.validate_resource_cleanup(baseline, current, fingerprint)
        with self.assertRaises(RuntimeError):
            resource.validate_resource_cleanup(with_cache, baseline, fingerprint)

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
