"""Hostless read-only pilot topology and guest-kernel route-table checks."""

import importlib.util
import io
import json
import subprocess
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

path = Path(__file__).with_name("opencode-pilot-preflight.py")
spec = importlib.util.spec_from_file_location("pilot_preflight", path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

GOOD_ROUTE = (
    "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n"
    "eth0\t00002A0A\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n"
)
DEFAULT_ROUTE = (
    "eth0\t00000000\t01002A0A\t0003\t0\t0\t0\t00000000\t0\t0\t0\n"
)
MANAGED = {
    "user.aginctus.managed": "true",
    "user.aginctus.resource": "workload",
    "user.aginctus.role": "agent-workload",
    "user.aginctus.workload": "dev",
}
DEVICES = {
    "root": {"type": "disk", "path": "/", "pool": "test"},
    "management": {"type": "nic", "network": "pilot-mgmt", "name": "eth0"},
}


def response(*, network=True, nat="false", routing="false", ipv6="none",
             status="Running", labels=None, devices=None, routes=GOOD_ROUTE):
    commands = []

    def fake(*argv):
        commands.append(argv)
        if argv[:3] == ("aginctus", "config", "get"):
            return {
                "incus.management.network.name": '"pilot-mgmt"',
                "infrastructure.herdr.name": '"pilot-herdr"',
                "workloads.dev.instance.name": '"pilot-dev"',
            }[argv[3]]
        if argv[:2] == ("incus", "query"):
            path = argv[2]
            if "/networks/" in path:
                result = {
                    "type": "bridge" if network else "physical",
                    "config": {"ipv4.nat": nat, "ipv4.routing": routing,
                               "ipv6.address": ipv6},
                }
            else:
                result = {"type": "container", "status": status}
                if path == "/1.0/instances/pilot-dev?project=default":
                    result["config"] = MANAGED if labels is None else labels
                    result["expanded_devices"] = DEVICES if devices is None else devices
            return json.dumps({"metadata": result})
        if argv == (
            "incus", "--project", "default", "exec", "pilot-dev", "--",
            "cat", "/proc/net/route",
        ):
            return routes
        raise AssertionError("unexpected command")
    return fake, commands


class PreflightTests(unittest.TestCase):
    def test_valid_readonly_topology_and_route_readback(self):
        fake, commands = response()
        with patch.object(module, "run", side_effect=fake), redirect_stdout(io.StringIO()) as out:
            module.verify()
        self.assertIn("PASS: read-only", out.getvalue())
        self.assertIn("NOT VERIFIED", out.getvalue())
        self.assertIn("actual packet denial", out.getvalue())
        self.assertEqual(commands[-1], (
            "incus", "--project", "default", "exec", "pilot-dev", "--",
            "cat", "/proc/net/route",
        ))

    def test_rejects_unsafe_bridge_before_guest_exec(self):
        for options in ({"nat": "true"}, {"routing": "true"},
                        {"ipv6": "auto"}, {"network": False}):
            fake, calls = response(**options)
            with (
                self.subTest(options=options),
                patch.object(module, "run", side_effect=fake),
                self.assertRaises(RuntimeError),
            ):
                module.verify()
            self.assertFalse(any("exec" in cmd for cmd in calls))

    def test_rejects_unowned_workload_and_extra_networks_before_guest_exec(self):
        cases = [
            {"labels": {}},
            {"labels": {**MANAGED, "user.aginctus.workload": "other"}},
            {"devices": {}},
            {"devices": {**DEVICES, "extra": {
                "type": "nic", "network": "public", "name": "eth1",
            }}},
            {"devices": {**DEVICES, "untrusted": {
                "type": "proxy", "listen": "tcp:0.0.0.0:8000",
            }}},
            {"devices": {**DEVICES, "management": {
                "type": "nic", "network": "public", "name": "eth0",
            }}},
            {"devices": {**DEVICES, "management": {
                "type": "nic", "network": "pilot-mgmt", "name": "eth1",
            }}},
        ]
        for options in cases:
            fake, calls = response(**options)
            with (
                self.subTest(options=options),
                patch.object(module, "run", side_effect=fake),
                self.assertRaises(RuntimeError),
            ):
                module.verify()
            self.assertFalse(any("exec" in cmd for cmd in calls))

    def test_rejects_unsafe_or_unknown_guest_routes(self):
        for raw in (
            GOOD_ROUTE + DEFAULT_ROUTE,
            GOOD_ROUTE.replace("eth0", "eth1"),
            GOOD_ROUTE.replace("0001", "garbage"),
            GOOD_ROUTE.replace("00FFFFFF", "not_hex"),
            GOOD_ROUTE.replace("00000000", "??????"),
            GOOD_ROUTE.splitlines()[0] + "\n",
            "",
            "network unreachable\n",
            GOOD_ROUTE + ("eth0\t00002A0A\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n" * 1025),
        ):
            with self.subTest(raw=raw[:75]), self.assertRaises(RuntimeError):
                module.verify_ipv4_routes(raw)

    def test_rejects_stopped_instance(self):
        fake, calls = response(status="Stopped")
        with patch.object(module, "run", side_effect=fake), self.assertRaisesRegex(
            RuntimeError, "not running"
        ):
            module.verify()
        self.assertFalse(any("exec" in cmd for cmd in calls))

    def test_rejects_missing_or_invalid_resource_name(self):
        with (
            patch.object(module, "run", return_value='"../other"'),
            self.assertRaisesRegex(RuntimeError, "invalid configured"),
        ):
            module.verify()

    def test_readonly_command_failures_are_redacted(self):
        with (
            patch.object(
                module.subprocess, "run",
                side_effect=subprocess.CalledProcessError(1, ["incus"], stderr="sensitive"),
            ),
            self.assertRaisesRegex(RuntimeError, "read-only query failed") as caught,
        ):
            module.run("incus", "query", "test")
        self.assertNotIn("sensitive", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
