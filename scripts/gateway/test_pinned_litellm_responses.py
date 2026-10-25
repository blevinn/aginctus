"""Pure offline tests for stock LiteLLM native Responses audit harness."""
import importlib.util
import json
import unittest
from pathlib import Path
from unittest.mock import patch

path = Path(__file__).with_name("probe_pinned_litellm_responses.py")
spec = importlib.util.spec_from_file_location("pinned_responses_probe", path)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)

POLICY = {"model_list": [], "litellm_settings": {
    "model_group_settings": {"forward_client_headers_to_llm_api": [probe.ALIAS]}}}


class PinnedResponsesProbeTests(unittest.TestCase):
    def test_podman_signature_policy_is_exactly_one_tag_and_default_deny(self):
        policy_path = Path(__file__).resolve().parents[2] / (
            "deploy/litellm-go/podman-probe-policy.json")
        policy = json.loads(policy_path.read_text(encoding="utf-8"))
        self.assertEqual(policy, {
            "default": [{"type": "reject"}],
            "transports": {"docker": {
                "ghcr.io/berriai/litellm:v1.103.0-stable": [
                    {"type": "insecureAcceptAnything"},
                ],
            }},
        })

    def test_model_group_scope_must_be_exact(self):
        probe.verify_policy(POLICY)
        for groups in (["*"], [probe.OTHER], [probe.ALIAS, probe.OTHER]):
            with self.subTest(groups=groups), self.assertRaises(RuntimeError):
                probe.verify_policy({"model_list": [], "litellm_settings": {
                    "model_group_settings": {"forward_client_headers_to_llm_api": groups}}})
        with self.assertRaises(RuntimeError):
            probe.verify_policy({**POLICY, "general_settings": {
                "forward_client_headers_to_llm_api": True}})

    def test_routes_never_point_to_real_upstream(self):
        rendered = probe.render_config(POLICY, 19876)
        self.assertEqual({route["model_name"] for route in rendered["model_list"]},
                         {probe.ALIAS, probe.OTHER})
        for route in rendered["model_list"]:
            params = route["litellm_params"]
            self.assertEqual(params["api_base"], "http://127.0.0.1:19876/v1")
            self.assertEqual(params["api_key"], probe.UPSTREAM_KEY)
        self.assertEqual(rendered["general_settings"]["master_key"], probe.PROXY_KEY)
        self.assertNotEqual(probe.PROXY_KEY, probe.UPSTREAM_KEY)

    def test_isolation_requires_opt_in_and_no_network_interface(self):
        with patch.dict(probe.os.environ, {}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "opt-in absent"):
                probe.verify_isolation()
        with patch.dict(probe.os.environ, {
            "AGINCTUS_ISOLATED_LITELLM_PROBE": "1"
        }, clear=True), patch.object(probe.os, "listdir", return_value=["lo", "eth0"]):
            with self.assertRaisesRegex(RuntimeError, "network isolation absent"):
                probe.verify_isolation()

    def sample_calls(self, *, ua="opencode/1.15.11", copied=False):
        result = []
        for session in (probe.SESSION_A, probe.SESSION_A, probe.SESSION_B, None):
            headers = {
                "authorization": "Bearer " + probe.UPSTREAM_KEY,
                "user-agent": ua,
            }
            if session:
                headers["x-opencode-session"] = session
                if copied:
                    headers["x-unapproved-probe"] = probe.SENTINELS["x-unapproved-probe"]
            result.append({"path": "/v1/responses", "headers": headers})
        return result

    def test_explicit_risk_classification_and_strict_success(self):
        self.assertTrue(probe.evaluate(self.sample_calls(), report=False))
        self.assertFalse(probe.evaluate(self.sample_calls(copied=True), report=False))
        self.assertFalse(probe.evaluate(self.sample_calls(ua="python-httpx/0.28"), report=False))

    def test_reject_wrong_auth_and_session_contamination(self):
        calls = self.sample_calls()
        calls[1]["headers"]["x-opencode-session"] = probe.SESSION_B
        with self.assertRaisesRegex(RuntimeError, "session"):
            probe.evaluate(calls, report=False)
        calls = self.sample_calls()
        calls[0]["headers"]["authorization"] = "Bearer " + probe.PROXY_KEY
        with self.assertRaisesRegex(RuntimeError, "upstream authorization"):
            probe.evaluate(calls, report=False)
        calls = self.sample_calls()
        calls[3]["headers"]["x-opencode-session"] = probe.SESSION_A
        with self.assertRaisesRegex(RuntimeError, "non-Go"):
            probe.evaluate(calls, report=False)

    def test_reject_unrelated_model_client_metadata_leak(self):
        calls = self.sample_calls()
        calls[3]["headers"]["x-unapproved-probe"] = (
            probe.SENTINELS["x-unapproved-probe"])
        with self.assertRaisesRegex(RuntimeError, "non-Go model forwarded"):
            probe.evaluate(calls, report=False)

    def test_streaming_response_terminal_and_synthetic_only(self):
        events = probe.response_events()
        self.assertIn(b"event: response.completed", events)
        self.assertIn(probe.MARKER.encode(), events)
        self.assertNotIn(probe.PROXY_KEY.encode(), events)
        self.assertNotIn(probe.UPSTREAM_KEY.encode(), events)


if __name__ == "__main__":
    unittest.main()
