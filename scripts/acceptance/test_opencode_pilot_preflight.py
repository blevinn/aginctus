#!/usr/bin/env python3
"""No Incus daemon or model provider is needed."""
import importlib.util
import io
from contextlib import redirect_stdout
from pathlib import Path
import unittest
from unittest.mock import patch

path = Path(__file__).with_name("opencode-pilot-preflight.py")
spec = importlib.util.spec_from_file_location("pilot_preflight", path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

def response(network=True, nat="false", routing="false", ipv6="none", status="Running"):
    def fake(*argv):
        if argv[:3] == ("aginctus", "config", "get"):
            return {
                "incus.management.network.name": '"pilot-mgmt"',
                "infrastructure.herdr.name": '"pilot-herdr"',
                "workloads.dev.instance.name": '"pilot-dev"',
            }[argv[3]]
        if argv[:2] == ("incus", "query"):
            path = argv[2]
            if "/networks/" in path:
                result = {"type": "bridge" if network else "physical",
                          "config": {"ipv4.nat": nat, "ipv4.routing": routing,
                                     "ipv6.address": ipv6}}
            else:
                result = {"type": "container", "status": status}
            import json
            return json.dumps({"metadata": result})
        raise AssertionError("unexpected command")
    return fake

class PreflightTests(unittest.TestCase):
    def test_valid_readonly_topology(self):
        with patch.object(module, "run", side_effect=response()), redirect_stdout(io.StringIO()) as output:
            module.verify()
        self.assertIn("PASS: read-only", output.getvalue())
        self.assertIn("NOT VERIFIED", output.getvalue())

    def test_rejects_unsafe_network(self):
        for options in ({"nat": "true"}, {"routing": "true"},
                        {"ipv6": "auto"}, {"network": False}):
            with self.subTest(options=options), patch.object(module, "run", side_effect=response(**options)):
                with self.assertRaises(RuntimeError):
                    module.verify()

    def test_rejects_stopped_instance(self):
        with patch.object(module, "run", side_effect=response(status="Stopped")):
            with self.assertRaisesRegex(RuntimeError, "not running"):
                module.verify()

    def test_rejects_missing_or_invalid_resource_name(self):
        with patch.object(module, "run", return_value='"../other"'):
            with self.assertRaisesRegex(RuntimeError, "invalid configured"):
                module.verify()

    def test_readonly_command_failures_are_redacted(self):
        import subprocess
        with patch.object(module.subprocess, "run", side_effect=subprocess.CalledProcessError(1, ["incus"])):
            with self.assertRaisesRegex(RuntimeError, "read-only query failed"):
                module.run("incus", "query", "test")

if __name__ == "__main__":
    unittest.main()
