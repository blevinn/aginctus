"""No Incus daemon, public network or provider used by these tests."""

import contextlib
import importlib.util
import io
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location(
    "guest_network_controls", Path(__file__).with_name("guest-network-controls.py")
)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)


def checked(mode, status="pass", *, code=None, extras=None):
    check = "gateway_tcp" if mode == "gateway" else "direct_public_no_route"
    document = {"schema_version": 1, "profile": "guest_tcp_probe",
                "check": check, "status": status, **(extras or {})}
    return subprocess.CompletedProcess(
        args=["incus"], returncode=(0 if status == "pass" else 1) if code is None else code,
        stdout=json.dumps(document), stderr="PRIVATE SECRET",
    )


def configured():
    return subprocess.CompletedProcess(
        args=["incus"], returncode=0,
        stdout=json.dumps({
            "endpoint": "http://10.42.40.9:4000/v1",
            "protocol": "responses",
            "model": "approved-alias",
        }), stderr="PRIVATE SECRET",
    )


class ControlsTests(unittest.TestCase):
    def setUp(self):
        self.pilot = Mock()
        self.pilot.config.return_value = "aginctus-dev"

    def result(self, *, gateway=checked("gateway"), public=checked("direct"),
               config=None, host_status="pass"):
        calls = []

        def process(argv):
            calls.append(argv)
            if argv[-3:] == [mod.GUEST_PROGRAM, "gateway", "10.42.40.9"]:
                return gateway
            if argv[-3:] == [mod.GUEST_PROGRAM, "direct", "1.1.1.1"]:
                return public
            if argv[-2:] == ["cat", mod.GUEST_CONFIG]:
                return configured() if config is None else config
            raise AssertionError("unexpected guest command")

        with (
            patch.object(mod, "execute", side_effect=process),
            patch.object(mod, "host_control", return_value=host_status) as baseline,
        ):
            result = mod.inspect("10.42.40.9", "1.1.1.1", self.pilot)
        return result, calls, baseline

    def test_exact_observed_controls_remain_incomplete_acceptance(self):
        result, calls, baseline = self.result()
        self.assertEqual(result["profile"], "guest_network_controls")
        self.assertEqual(result["status"], "incomplete")
        self.assertTrue(all(r["status"] == "pass" for r in result["results"]))
        self.assertEqual(result["credential_egress"], "not_verified")
        self.assertEqual(result["upstream_inference"], "not_verified")
        self.assertFalse(result["complete_acceptance"])
        self.pilot.verify.assert_called_once_with()
        self.pilot.config.assert_called_once_with("workloads.dev.instance.name")
        baseline.assert_called_once_with("1.1.1.1")
        self.assertEqual(len(calls), 3)
        self.assertEqual(calls[0], mod.guest_args(
            "aginctus-dev", "cat", mod.GUEST_CONFIG
        ))
        self.assertEqual(calls[1], mod.guest_args(
            "aginctus-dev", mod.GUEST_PROGRAM, "gateway", "10.42.40.9"
        ))
        self.assertEqual(calls[2], mod.guest_args(
            "aginctus-dev", mod.GUEST_PROGRAM, "direct", "1.1.1.1"
        ))
        self.assertNotIn("PRIVATE SECRET", json.dumps(result))
        self.assertNotIn("10.42.40", json.dumps(result))

    def test_bad_guest_gateway_is_not_accepted_as_public_negative_proof(self):
        result, calls, baseline = self.result(gateway=checked("gateway", "error"))
        self.assertEqual(result["status"], "error")
        self.assertEqual(result["results"][2]["status"], "error")
        self.assertEqual(result["results"][4]["status"], "not_run")
        self.assertEqual(len(calls), 2)
        baseline.assert_not_called()

    def test_public_control_failure_never_runs_direct_guest_probe(self):
        result, calls, baseline = self.result(host_status="error")
        self.assertEqual(result["results"][3]["status"], "error")
        self.assertEqual(result["results"][4]["status"], "not_run")
        self.assertEqual(len(calls), 2)
        baseline.assert_called_once()

    def test_public_connectivity_is_a_failure_not_a_negative_pass(self):
        result, calls, _ = self.result(public=checked("direct", "fail"))
        self.assertEqual(result["status"], "fail")
        self.assertEqual(result["results"][-1]["status"], "fail")
        self.assertEqual(len(calls), 3)

    def test_malformed_spoofed_or_contradictory_guest_json_rejected(self):
        for payload in (
            checked("direct", "pass", code=1),
            checked("direct", "fail", code=0),
            checked("gateway", "pass"),
            checked("direct", "pass", extras={"secret": "PRIVATE SECRET"}),
            subprocess.CompletedProcess(["incus"], 0, "{not-json}", "secret"),
            subprocess.CompletedProcess(["incus"], 0, '{"status":"pass","status":"fail"}', ""),
        ):
            with self.subTest(payload=payload.stdout[:35]):
                with patch.object(mod, "execute", return_value=payload):
                    with self.assertRaises(mod.ProbeError):
                        mod.guest_probe("aginctus-dev", "direct", "1.1.1.1")

    def test_bad_config_blocks_guest_network_probe(self):
        bad = subprocess.CompletedProcess(
            ["incus"], 0,
            json.dumps({"endpoint": "http://10.42.40.10:4000/v1",
                        "protocol": "responses", "model": "approved-alias"}), "",
        )
        result, calls, baseline = self.result(config=bad)
        self.assertEqual(result["results"][1]["status"], "error")
        self.assertEqual(len(calls), 1)
        baseline.assert_not_called()

    def test_preflight_failure_never_enters_guest(self):
        self.pilot.verify.side_effect = RuntimeError("PRIVATE SECRET")
        with (
            patch.object(mod, "execute") as guest,
            patch.object(mod, "host_control") as external,
        ):
            result = mod.inspect("10.42.40.9", "1.1.1.1", self.pilot)
        self.assertEqual(result["results"][0]["status"], "error")
        guest.assert_not_called()
        external.assert_not_called()
        self.assertNotIn("PRIVATE SECRET", json.dumps(result))

    def test_missing_opt_in_and_dry_run_have_no_external_effect(self):
        args = ["--gateway-ip", "10.42.40.9", "--public-ip", "1.1.1.1"]
        for selected, approval in ((False, "I_UNDERSTAND"), (True, "")):
            with self.subTest(selected=selected):
                argv = [*args, "--run"] if selected else args
                out = io.StringIO()
                with (
                    patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_EGRESS": approval}),
                    patch.object(mod, "inspect") as inspect,
                    contextlib.redirect_stdout(out),
                ):
                    code = mod.main(argv)
                inspect.assert_not_called()
                self.assertEqual(code, 1 if selected else 0)
                self.assertEqual(
                    json.loads(out.getvalue())["status"],
                    "error" if selected else "incomplete",
                )
        with (
            patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_EGRESS": "I_UNDERSTAND"}),
            patch.object(mod, "inspect", return_value=mod.report(
                dict.fromkeys(mod.CHECKS, "pass")
            )),
            contextlib.redirect_stdout(io.StringIO()) as out,
        ):
            self.assertEqual(mod.main([*args, "--run"]), 0)
        self.assertFalse(json.loads(out.getvalue())["complete_acceptance"])

    def test_selectors_reject_dns_loopback_private_public_and_bad_ports(self):
        for mode, value in (
            ("gateway", "localhost"), ("gateway", "127.0.0.1"),
            ("gateway", "1.1.1.1"), ("gateway", "10.1.1.1:4000"),
            ("public", "10.42.40.1"), ("public", "127.0.0.1"),
            ("public", "169.254.1.1"), ("public", "1.1.1.1\n"),
            ("public", "example.com"),
        ):
            with self.subTest(mode=mode, value=value), self.assertRaises(mod.ProbeError):
                mod.target(mode, value)

    def test_host_baseline_suppresses_socket_failure(self):
        with patch.object(mod.socket, "create_connection",
                          side_effect=OSError("PRIVATE SECRET")):
            self.assertEqual(mod.host_control("1.1.1.1"), "error")

    def test_fixed_inc_us_command_output_is_bounded(self):
        response = subprocess.CompletedProcess(["incus"], 0, "x" * 4097, "PRIVATE")
        with patch.object(mod.subprocess, "run", return_value=response):
            with self.assertRaises(mod.ProbeError):
                mod.execute(["incus"])
        with patch.object(mod.subprocess, "run",
                          side_effect=subprocess.TimeoutExpired(["incus"], 1)):
            with self.assertRaises(mod.ProbeError):
                mod.execute(["incus"])


if __name__ == "__main__":
    unittest.main()
