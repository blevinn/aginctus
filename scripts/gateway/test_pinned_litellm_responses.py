"""Pure offline tests for stock LiteLLM native Responses audit harness."""
import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

path = Path(__file__).with_name("probe_pinned_litellm_responses.py")
spec = importlib.util.spec_from_file_location("pinned_responses_probe", path)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)

POLICY = {"model_list": [], "litellm_settings": {
    "model_group_settings": {"forward_client_headers_to_llm_api": [probe.ALIAS]}}}


class PinnedResponsesProbeTests(unittest.TestCase):
    def test_real_completion_requires_native_identity_terminal_stop_and_text(self):
        session = "ses_test_generated_identity"
        events = [
            {"type": "step_start", "sessionID": session},
            {"type": "text", "sessionID": session, "part": {"text": probe.MARKER}},
            {"type": "step_finish", "sessionID": session, "part": {"reason": "stop"}},
        ]

        def output(rows):
            return "\n".join(json.dumps(row) for row in rows)

        self.assertEqual(probe.completed_native_session(output(events)), session)
        self.assertEqual(probe.completed_native_session(output(events), session), session)
        for rows in (events[:2], events[1:2], events[::2], [],
                     [*events, {"type": "error"}],
                     [*events, {"type": "text", "sessionID": "ses_another_identity"}],
                     [{**row, "sessionID": probe.SESSION_A} for row in events],
                     [{**row, "sessionID": "bad"} for row in events]):
            with self.subTest(rows=rows), self.assertRaises(RuntimeError):
                probe.completed_native_session(output(rows))
        with self.assertRaises(RuntimeError):
            probe.completed_native_session(output(events), "ses_wrong_resumed_identity")
        for invalid in ("not-json", "[]", "null"):
            with self.subTest(invalid=invalid), self.assertRaises(RuntimeError):
                probe.completed_native_session(invalid)

    def test_missing_session_is_absent_on_wire(self):
        response = MagicMock()
        response.__enter__.return_value = response
        response.read.return_value = b""
        with patch.object(probe.OPENER, "open", return_value=response) as op:
            probe.send(12345, probe.ALIAS, None)
        headers = {k.lower(): v for k, v in op.call_args.args[0].header_items()}
        self.assertNotIn("x-opencode-session", headers)

    def test_packaged_bundle_rejects_missing_or_modified_files_before_import(self):
        root = Path(__file__).parent
        assets = {"config.yaml": (root / "selective-go.yaml").read_bytes()}
        for name in ("litellm_opencode_go_hook.py", "opencode_go_forwarding.py"):
            assets[name] = (root / name).read_bytes()
        manifest = {name: hashlib.sha256(data).hexdigest() for name, data in assets.items()}
        for name in assets:
            for missing in (True, False):
                with self.subTest(name=name, missing=missing), tempfile.TemporaryDirectory() as tmp:
                    bundle = Path(tmp)
                    for asset, data in assets.items():
                        (bundle / asset).write_bytes(data)
                    (bundle / "manifest.json").write_text(json.dumps(manifest))
                    if missing:
                        (bundle / name).unlink()
                    else:
                        (bundle / name).write_bytes(b"tampered")
                    with self.assertRaises((OSError, RuntimeError)):
                        probe.load_packaged_hook(bundle)

    def test_packaged_policy_rejects_broad_forwarding(self):
        policy = {"litellm_settings": {
            "callbacks": ["litellm_opencode_go_hook.proxy_handler_instance"],
        }}
        probe.verify_packaged_policy(policy)
        policy["litellm_settings"]["forward_client_headers_to_llm_api"] = True
        with self.assertRaisesRegex(RuntimeError, "callback-only"):
            probe.verify_packaged_policy(policy)

    def test_operator_runbook_uses_ephemeral_podman_policy(self):
        runbook_path = Path(__file__).resolve().parents[2] / (
            "docs/gateway-api-key-pilot.md")
        runbook = runbook_path.read_text(encoding="utf-8")
        self.assertIn(
            'policy=$(mktemp)',
            runbook,
        )
        self.assertNotIn(
            'podman-probe-policy.json',
            runbook,
        )

    def test_probe_image_matches_aginctus_gateway_pin(self):
        repo_root = Path(__file__).resolve().parents[2]
        gateway_settings = (repo_root / "nix/settings-options.nix").read_text(
            encoding="utf-8")
        self.assertIn('default = "ghcr.io/berriai/litellm:v1.103.0";',
                      gateway_settings)
        runbook = (repo_root / "docs/gateway-api-key-pilot.md").read_text(
            encoding="utf-8")
        self.assertIn("ghcr.io/berriai/litellm:v1.103.0", runbook)
        self.assertNotIn("ghcr.io/berriai/litellm:v1.103.0-stable", runbook)

    def test_probe_does_not_ship_an_insecure_signature_policy(self):
        policy_path = Path(__file__).resolve().parents[2] / (
            "deploy/litellm-go/podman-probe-policy.json")
        self.assertFalse(policy_path.exists())

    def test_anonymous_control_has_no_alternative_auth_headers(self):
        response = MagicMock()
        response.status = 403
        response.read.return_value = b""
        response.__enter__.return_value = response
        for authenticated in (False, True):
            with self.subTest(authenticated=authenticated), patch.object(
                probe.OPENER, "open", return_value=response
            ) as op:
                status, _ = probe.send(12345, probe.ALIAS, probe.SESSION_A,
                                       authenticated=authenticated)
                self.assertEqual(status, 403)
                request = op.call_args.args[0]
                headers = {k.lower(): v for k, v in request.header_items()}
                self.assertEqual("authorization" in headers, authenticated)
                self.assertEqual("x-api-key" in headers, authenticated)
                self.assertEqual("x-unapproved-probe" in headers, authenticated)

    def test_anonymous_auth_gate_demands_401_403_and_no_upstream(self):
        for code in (401, 403):
            probe.require_unauthenticated_rejection(code, 0)
        for code, upstream in ((200, 1), (200, 0), (400, 0), (404, 0),
                               (500, 0), (401, 1), (403, 1)):
            with self.subTest(code=code, upstream=upstream), self.assertRaisesRegex(
                RuntimeError, "http_status=.*upstream_calls="
            ):
                probe.require_unauthenticated_rejection(code, upstream)

    def test_selective_mode_loads_exact_hook_without_any_broad_forwarding(self):
        route = probe.render_config(POLICY, 8765, selective_hook=True)
        self.assertEqual(route["litellm_settings"], {
            "callbacks": ["litellm_opencode_go_hook.proxy_handler_instance"],
        })
        self.assertNotIn("model_group_settings", route["litellm_settings"])
        self.assertEqual({entry["model_name"] for entry in route["model_list"]},
                         {probe.ALIAS, probe.OTHER})

    def test_selective_negative_controls_require_fourxx_and_no_upstream(self):
        for status in (400, 401, 403, 422):
            with self.subTest(status=status), patch.object(
                probe, "send", return_value=(status, b"")
            ):
                probe.MockGo.calls = []
                probe.check_rejected_without_upstream(
                    1234, probe.SESSION_A, user_agent="bogus/1.0")
        for status in (200, 500):
            with self.subTest(status=status), patch.object(
                probe, "send", return_value=(status, b"")
            ), self.assertRaisesRegex(RuntimeError, "failed closed"):
                probe.MockGo.calls = []
                probe.check_rejected_without_upstream(
                    1234, probe.SESSION_A, user_agent="bogus/1.0")
        probe.MockGo.calls = [{}]

        def recorded(*_args, **_kwargs):
            probe.MockGo.calls.append({})
            return 403, b""

        with patch.object(probe, "send", side_effect=recorded), self.assertRaisesRegex(
            RuntimeError, "upstream_delta=1"
        ):
            probe.check_rejected_without_upstream(
                1234, probe.SESSION_A, user_agent="bogus/1.0")

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
        with patch.dict(probe.os.environ, {}, clear=True), self.assertRaisesRegex(
            RuntimeError, "opt-in absent"
        ):
            probe.verify_isolation()
        with patch.dict(probe.os.environ, {
            "AGINCTUS_ISOLATED_LITELLM_PROBE": "1"
        }, clear=True), patch.object(
            probe.os, "listdir", return_value=["lo", "eth0"]
        ), self.assertRaisesRegex(RuntimeError, "network isolation absent"):
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
