"""Offline tests: never contact LiteLLM or any real provider."""
import contextlib
import importlib.util
import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "spend_inspector", Path(__file__).with_name("inspect_responses_spend.py")
)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

RUN = "aginctus-acceptance.ABCD1234"
SESSION = "ses_abcdefgh12345678"
REQUEST = "resp_abcdefgh12345678"
MODEL = "aginctus-go-pilot"
KEY = "sk-pilot-workload-1234567890"
MASTER = "sk-private-master-1234567890"


class FakeOpener:
    def __init__(self, response):
        self.body = json.dumps(response).encode() if not isinstance(response, bytes) else response
        self.requests = []

    def open(self, req, timeout):
        self.requests.append((req, timeout))
        body = self.body
        class Response:
            status = 200
            def __enter__(self):
                return self
            def __exit__(self, *_):
                return None
            def read(self, length):
                return body[:length]
        return Response()


class SpendEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.key_file = root / "key"
        self.key_file.write_text(KEY + "\n")
        self.key_file.chmod(0o600)
        secrets = root / "aginctus/gateway/local/secrets.json"
        secrets.parent.mkdir(parents=True)
        secrets.write_text(json.dumps({"AGINCTUS_GATEWAY_MASTER_KEY": MASTER}))
        secrets.chmod(0o600)
        self.env = patch.dict(os.environ, {"XDG_STATE_HOME": self.temp.name})
        self.env.start()
        self.addCleanup(self.env.stop)

    def row(self, **changes):
        import hashlib
        return {"request_id": REQUEST, "session_id": SESSION, "end_user": RUN,
                "model_group": MODEL, "api_key": hashlib.sha256(KEY.encode()).hexdigest(),
                "call_type": "aresponses", **changes}

    def inspect(self, response):
        opener = FakeOpener(response)
        return mod.inspect(host="http://10.0.0.9:4000", gateway_id="local",
                           request_id=REQUEST, run_id=RUN, session_id=SESSION,
                           model_alias=MODEL, key_file=str(self.key_file), opener=opener), opener

    def test_exact_correlated_log_without_inference_promotion(self):
        result, opener = self.inspect([self.row(status="success", proxy_server_request={"headers": {"Authorization": "Bearer SECRET"}})])
        self.assertEqual(result, {"gateway_log_correlated": "pass", "upstream_inference": "not_verified",
                                  "agent_tool_execution": "not_verified", "complete_acceptance": False})
        self.assertEqual(len(opener.requests), 1)
        req, timeout = opener.requests[0]
        self.assertEqual(req.full_url, "http://10.0.0.9:4000/spend/logs?request_id=" + REQUEST)
        self.assertEqual(req.get_method(), "GET")
        self.assertEqual(timeout, 15)
        self.assertNotIn("SECRET", json.dumps(result))

    def test_diagnostic_reports_only_field_statuses_and_never_promotes(self):
        row = self.row(
            session_id=None, end_user="another-run", model_group="",
            status="success", proxy_server_request={
                "headers": {"Authorization": "Bearer PRIVATE_PROVIDER_KEY"},
            },
        )
        output, opener = self.inspect_diagnostic([row])
        self.assertEqual(output["profile"], "gateway_spend_diagnostic")
        self.assertEqual(output["status"], "incomplete")
        self.assertEqual(output["gateway_log_correlated"], "not_verified")
        self.assertEqual(output["spend_log_status"], "success_label")
        self.assertFalse(output["complete_acceptance"])
        self.assertEqual(output["upstream_inference"], "not_verified")
        self.assertEqual(output["agent_tool_execution"], "not_verified")
        self.assertEqual(output["fields"], [
            {"id": "request_id", "status": "match"},
            {"id": "session_id", "status": "missing"},
            {"id": "end_user", "status": "mismatch"},
            {"id": "model_group", "status": "missing"},
            {"id": "api_key", "status": "match"},
            {"id": "call_type", "status": "match"},
        ])
        self.assertEqual(len(opener.requests), 1)
        for sensitive in ("PRIVATE_PROVIDER_KEY", "another-run", REQUEST, SESSION, KEY):
            self.assertNotIn(sensitive, json.dumps(output))

    def inspect_diagnostic(self, response):
        opener = FakeOpener(response)
        result = mod.inspect(
            host="http://10.0.0.9:4000", gateway_id="local",
            request_id=REQUEST, run_id=RUN, session_id=SESSION,
            model_alias=MODEL, key_file=str(self.key_file),
            opener=opener, diagnose=True,
        )
        return result, opener

    def test_diagnostics_equal_fields_still_cannot_prove_upstream(self):
        for observed in ("success", "failure", "unknown", None):
            with self.subTest(status=observed):
                result, _ = self.inspect_diagnostic([self.row(status=observed)])
                self.assertEqual({x["status"] for x in result["fields"]}, {"match"})
                self.assertEqual(result["gateway_log_correlated"], "not_verified")
                self.assertEqual(result["upstream_inference"], "not_verified")
                self.assertEqual(result["status"], "incomplete")
                self.assertFalse(result["complete_acceptance"])
                expected = {
                    "success": "success_label", "failure": "failure_label",
                }.get(observed, "unrecognized")
                self.assertEqual(result["spend_log_status"], expected)

    def test_diagnostics_still_reject_ambiguous_malformed_and_missing_rows(self):
        for rows in ([], [self.row(), self.row()], {}, b"not-json",
                     b'[{"request_id":"a","request_id":"b"}]', b"[" + b"x" * 65537):
            with self.subTest(response=str(rows)[:28]), self.assertRaises(mod.ProbeError):
                self.inspect_diagnostic(rows)

    def test_diagnostic_cli_uses_exactly_one_read_only_get_and_redacts_failures(self):
        args = ["--endpoint", "http://10.0.0.9:4000", "--gateway-id", "local",
                "--request-id", REQUEST, "--run-id", RUN, "--session-id", SESSION,
                "--model-alias", MODEL, "--workload-key-file", str(self.key_file)]
        diagnostic = io.StringIO()
        fake = FakeOpener([self.row(session_id=None, status="failure")])
        with (
            patch.object(mod, "build_opener", return_value=fake),
            contextlib.redirect_stdout(diagnostic),
        ):
            self.assertEqual(mod.main([*args, "--diagnose"]), 0)
        out = json.loads(diagnostic.getvalue())
        self.assertEqual(out["status"], "incomplete")
        self.assertEqual(out["gateway_log_correlated"], "not_verified")
        self.assertEqual(out["spend_log_status"], "failure_label")
        self.assertEqual(len(fake.requests), 1)
        self.assertEqual(fake.requests[0][0].get_method(), "GET")
        self.assertNotIn(MASTER, diagnostic.getvalue())
        self.assertNotIn(KEY, diagnostic.getvalue())
        self.assertNotIn(SESSION, diagnostic.getvalue())

        bad = io.StringIO()
        with (
            patch.object(mod, "fetch_row", side_effect=mod.ProbeError("PROVIDER_SECRET")),
            contextlib.redirect_stdout(bad),
        ):
            self.assertEqual(mod.main([*args, "--diagnose"]), 1)
        failed = json.loads(bad.getvalue())
        self.assertEqual(failed, mod.diagnostic_error())
        self.assertEqual({row["status"] for row in failed["fields"]}, {"not_run"})
        self.assertNotIn("PROVIDER_SECRET", bad.getvalue())

        with self.assertRaises(SystemExit), contextlib.redirect_stderr(io.StringIO()):
            mod.main([*args, "--inspect", "--diagnose"])

    def test_bad_diagnostic_selectors_never_read_keys_or_contact_gateway(self):
        with (
            patch.object(mod, "_private_file") as secret,
            patch.object(mod, "fetch_row") as fetch,
            self.assertRaises(mod.ProbeError),
        ):
            mod.inspect(
                host="http://10.0.0.9:4000", gateway_id="local",
                request_id="../../unsafe", run_id=RUN, session_id=SESSION,
                model_alias=MODEL, key_file=str(self.key_file),
                diagnose=True,
            )
        secret.assert_not_called()
        fetch.assert_not_called()

    def test_wrong_or_missing_session_model_key_run_or_call_type_never_passes(self):
        for changes in ({"session_id": "ses_other"}, {"session_id": None},
                        {"end_user": "other"}, {"model_group": "different"},
                        {"api_key": "wrong"}, {"call_type": "acompletion"},
                        {"request_id": "resp_other"}):
            with self.subTest(changes=changes), self.assertRaises(mod.ProbeError):
                self.inspect([self.row(**changes)])

    def test_missing_duplicate_or_invalid_logs_fail_closed(self):
        for rows in ([], [self.row(), self.row()], {}, b"not JSON", b"[null]",
                     b'[ {"request_id":"a","request_id":"b"} ]', b"x" * 65537):
            with self.subTest(kind=str(rows)[:30]), self.assertRaises(mod.ProbeError):
                self.inspect(rows)

    def test_untrusted_selectors_do_not_read_secrets_or_contact_gateway(self):
        for change in ({"request_id": "../../secret"}, {"run_id": "invalid"},
                       {"session_id": "fake"}, {"model_alias": "hack\nheader"},
                       {"gateway_id": "../other"}, {"host": "http://example.com:4000"},
                       {"host": "http://192.0.0.2:4000"}):
            args = dict(host="http://10.0.0.9:4000", gateway_id="local",
                        request_id=REQUEST, run_id=RUN, session_id=SESSION,
                        model_alias=MODEL, key_file=str(self.key_file))
            args.update(change)
            with self.subTest(change=change), patch.object(mod, "_private_file") as key, self.assertRaises(mod.ProbeError):
                mod.inspect(**args)
            key.assert_not_called()

    def test_private_key_permissions_symlink_and_bad_state_rejected(self):
        key_link = Path(self.temp.name) / "link"
        key_link.symlink_to(self.key_file)
        for path in (key_link, self.key_file):
            with self.subTest(path=path):
                if path == self.key_file:
                    path.chmod(0o644)
                with self.assertRaises(mod.ProbeError):
                    self.inspect([self.row()]) if path == self.key_file else mod.inspect(
                        host="http://10.0.0.9:4000", gateway_id="local",
                        request_id=REQUEST, run_id=RUN, session_id=SESSION,
                        model_alias=MODEL, key_file=str(path))
        self.key_file.chmod(0o600)

    def test_dry_run_requires_no_credentials_network_and_safe_json(self):
        args = ["--endpoint", "http://10.0.0.9:4000", "--gateway-id", "local",
                "--request-id", REQUEST, "--run-id", RUN, "--session-id", SESSION,
                "--model-alias", MODEL, "--workload-key-file", "/missing"]
        output = io.StringIO()
        with patch.object(mod, "_private_file") as file, patch.object(mod, "fetch_row") as fetch, contextlib.redirect_stdout(output):
            self.assertEqual(mod.main(args), 0)
        file.assert_not_called()
        fetch.assert_not_called()
        self.assertEqual(json.loads(output.getvalue())["gateway_log_correlated"], "not_run")
        out = io.StringIO()
        with patch.object(mod, "fetch_row", side_effect=mod.ProbeError("SECRET payload")), contextlib.redirect_stdout(out):
            self.assertEqual(mod.main(args + ["--inspect"]), 1)
        self.assertEqual(json.loads(out.getvalue())["gateway_log_correlated"], "error")
        self.assertNotIn("SECRET", out.getvalue())


if __name__ == "__main__":
    unittest.main()
