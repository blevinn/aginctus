"""Offline contract cases for #122. No gateway or provider is contacted."""
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
    "gateway_smoke", Path(__file__).with_name("authenticated-inference-smoke.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)


def transcript(marker, state="completed", output=True):
    events = [f'event: response.created\ndata: {json.dumps({"type": "response.created"})}\n']
    if output:
        events.append('event: response.output_text.done\ndata: ' +
                      json.dumps({"type": "response.output_text.done", "text": marker}) + '\n')
    events.append('event: response.' + state + '\ndata: ' +
                  json.dumps({"type": "response." + state,
                              "response": {"status": state}}) + '\n')
    events.append('data: [DONE]\n')
    return ('\n'.join(events) + '\n').encode()


class MockOpener:
    def __init__(self, marker, visible=("coding",)):
        self.marker = marker
        self.visible = visible
        self.calls = []

    def open(self, req, timeout):
        self.calls.append(req)
        authorization = req.get_header("Authorization")
        if req.full_url.endswith("/v1/models") and authorization != "Bearer good-workload":
            raise mod.HTTPError(req.full_url, 401, "secret leaked in error", None, None)
        if req.full_url.endswith("/v1/models"):
            data = json.dumps({"data": [{"id": name} for name in self.visible]}).encode()
        elif req.full_url.endswith("/health/liveliness"):
            data = b"ok"
        elif req.full_url.endswith("/v1/responses"):
            request_body = json.loads(req.data)
            assert request_body["stream"] is True
            assert request_body["model"] == "coding"
            data = transcript(self.marker)
        else:
            raise AssertionError(req.full_url)

        class Response:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *_): pass
            def read(self, amount): return data[:amount]
        return Response()


class SmokeTests(unittest.TestCase):
    def test_endpoint_fails_closed(self):
        self.assertEqual(mod.endpoint("http://10.0.0.9:4000"), "http://10.0.0.9:4000")
        for address in ("https://10.0.0.9:4000", "http://1.2.3.4:4000",
                        "http://host.local:4000", "http://10.0.0.9:1234",
                        "http://name:pass@10.0.0.9:4000", "http://[::1]:4000"):
            with self.subTest(address=address), self.assertRaises(mod.SmokeError):
                mod.endpoint(address)
        with self.assertRaises(mod.SmokeError):
            mod.NoRedirect().redirect_request(None, None, 301, "", {}, "http://8.8.8.8/")

    def test_stream_requires_completed_terminal_and_actual_requested_text(self):
        marker = "AGINCTUS_OK_TEST123"
        self.assertEqual(mod.verify_sse(transcript(marker), marker), 3)
        for bad in (transcript(marker, "failed"), transcript(marker, "incomplete"),
                    transcript(marker, output=False), transcript(marker)[:-31],
                    transcript(marker) + b'event: response.created\ndata: {"type":"response.created"}\n\n',
                    transcript("OTHER_OUTPUT")):
            with self.subTest(bad=bad[:48]), self.assertRaises(mod.SmokeError):
                mod.verify_sse(bad, marker)

    def test_authenticated_live_flow_mock(self):
        marker = "AGINCTUS_OK_TEST123"
        fake = MockOpener(marker)
        got, count = mod.run("http://10.0.0.9:4000", "coding",
                             "good-workload", "master", fake, marker)
        self.assertEqual(got, marker)
        self.assertEqual(count, 3)
        self.assertEqual(len(fake.calls), 5)
        self.assertEqual(fake.calls[4].get_method(), "POST")
        with self.assertRaises(mod.SmokeError):
            mod.run("http://10.0.0.9:4000", "coding", "master", "master", fake, marker)
        self.assertEqual(len(fake.calls), 5)

    def test_authorization_only_is_read_only_and_requires_exact_scope(self):
        marker = "AGINCTUS_OK_TEST123"
        fake = MockOpener(marker)
        checked = mod.run_authorization(
            "http://10.0.0.9:4000", "coding", "good-workload", "master", fake
        )
        self.assertIs(checked, fake)
        self.assertEqual(len(fake.calls), 4)
        self.assertEqual(
            [req.full_url for req in fake.calls],
            ["http://10.0.0.9:4000/health/liveliness"]
            + ["http://10.0.0.9:4000/v1/models"] * 3,
        )
        self.assertTrue(all(req.get_method() == "GET" for req in fake.calls))
        self.assertTrue(all(req.data is None for req in fake.calls))
        self.assertNotIn("/v1/responses", " ".join(req.full_url for req in fake.calls))

        excessive = MockOpener(marker, visible=("coding", "unrelated"))
        with self.assertRaisesRegex(mod.SmokeError, "exact one-alias"):
            mod.run_authorization(
                "http://10.0.0.9:4000", "coding", "good-workload", "master", excessive
            )
        self.assertEqual(len(excessive.calls), 4)
        with self.assertRaises(mod.SmokeError):
            mod.run_authorization(
                "http://10.0.0.9:4000", "coding", "master", "master", excessive
            )
        self.assertEqual(len(excessive.calls), 4)

    def test_authorization_only_fails_if_anonymous_or_invalid_key_is_accepted(self):
        for unexpected in (0, 1):
            with self.subTest(unexpected=unexpected):
                responses = [
                    (200, b"ok"),
                    (200, b"unexpected") if unexpected == 0 else (401, b""),
                    (200, b"unexpected") if unexpected == 1 else (401, b""),
                ]
                with patch.object(mod, "request", side_effect=responses) as probe:
                    with self.assertRaisesRegex(mod.SmokeError, "missing/invalid key"):
                        mod.run_authorization(
                            "http://10.0.0.9:4000", "coding", "good-workload", "master",
                            opener=object(),
                        )
                self.assertLessEqual(probe.call_count, 3)
                self.assertNotIn("/v1/responses", " ".join(
                    call.args[2] for call in probe.call_args_list
                ))

    def test_authorization_only_cli_uses_protected_secrets_without_responses(self):
        with tempfile.TemporaryDirectory() as root:
            key = Path(root) / "workload"
            key.write_text("good-workload\n")
            key.chmod(0o600)
            state = Path(root) / "aginctus/gateway/local/secrets.json"
            state.parent.mkdir(parents=True)
            state.write_text(json.dumps({"AGINCTUS_GATEWAY_MASTER_KEY": "master"}))
            state.chmod(0o600)
            argv = [
                "--endpoint", "http://10.0.0.9:4000",
                "--alias", "coding", "--workload-key-file", str(key),
            ]
            out = io.StringIO()
            fake = MockOpener("AGINCTUS_OK_TEST123")
            with (
                patch.dict(os.environ, {"XDG_STATE_HOME": root}),
                patch.object(mod, "build_opener", return_value=fake),
                contextlib.redirect_stdout(out),
            ):
                self.assertEqual(mod.main([*argv, "--auth-only"]), 0)
            self.assertEqual(json.loads(out.getvalue()), {
                "gateway_authorization": "pass",
                "upstream_inference": "not_verified",
                "agent_tools": "not_verified",
                "credential_egress": "not_verified",
                "complete_acceptance": False,
            })
            self.assertEqual(len(fake.calls), 4)
            self.assertNotIn("good-workload", out.getvalue())
            self.assertNotIn("master", out.getvalue())
            out = io.StringIO()
            with (
                patch.dict(os.environ, {"XDG_STATE_HOME": root}),
                patch.object(mod, "build_opener", return_value=MockOpener(
                    "AGINCTUS_OK_TEST123", visible=("coding", "unrelated")
                )),
                contextlib.redirect_stdout(out),
                contextlib.redirect_stderr(io.StringIO()),
            ):
                self.assertEqual(mod.main([*argv, "--auth-only"]), 1)
            self.assertEqual(out.getvalue(), "")
            with self.assertRaises(SystemExit):
                mod.main([*argv, "--auth-only", "--run"])

    def test_http_error_classification_is_bounded_and_never_logs_provider_text(self):
        marker = "AGINCTUS_OK_TEST123"

        class FailedResponses(MockOpener):
            def __init__(self, response_body, status=400):
                super().__init__(marker)
                self.response_body = response_body
                self.status = status

            def open(self, req, timeout):
                if req.full_url.endswith("/v1/responses"):
                    raise mod.HTTPError(
                        req.full_url, self.status, "provider secret and prompt",
                        {}, io.BytesIO(self.response_body),
                    )
                return super().open(req, timeout)

        for payload, expected in (
            ({"error": {"type": "BadRequestError",
                        "message": "MissingSessionID: x-opencode-session required; private-prompt"}},
             "missing-session-id"),
            ({"error": {"message": "unsupported parameter: temperature"}},
             "unsupported-parameter"),
            ({"error": {"code": "model_not_found", "message": "secret-model"}},
             "unknown-model"),
            ({"error": {"type": "AuthenticationError", "message": "invalid api key secret"}},
             "provider-authentication"),
            ({"error": {"message": "some unexpected private prompt secret"}},
             "unclassified"),
        ):
            with self.subTest(expected=expected):
                fake = FailedResponses(json.dumps(payload).encode())
                with self.assertRaises(mod.SmokeError) as failure:
                    mod.run("http://10.0.0.9:4000", "coding",
                            "good-workload", "master", fake, marker)
                self.assertIn(expected, str(failure.exception))
                self.assertNotIn("private-prompt", str(failure.exception))
                self.assertNotIn("secret", str(failure.exception))
                self.assertNotIn("temperature", str(failure.exception))
                self.assertNotIn("provider secret", str(failure.exception))

        for body in (b"not JSON secret", b'{"error":"MissingSessionID"}',
                     b'{"error":{"message":"MissingSessionID"}}' + b"x" * 16384):
            fake = FailedResponses(body)
            with self.assertRaises(mod.SmokeError) as failure:
                mod.run("http://10.0.0.9:4000", "coding",
                        "good-workload", "master", fake, marker)
            self.assertIn("unclassified", str(failure.exception))
            self.assertNotIn("secret", str(failure.exception))

        # Never parse error bodies on missing/invalid-key probes.
        fake = MockOpener(marker)
        self.assertEqual(mod.run("http://10.0.0.9:4000", "coding",
                                 "good-workload", "master", fake, marker)[1], 3)

    def test_protected_file_and_dry_run(self):
        with tempfile.TemporaryDirectory() as td:
            key = Path(td) / "key"
            key.write_text("test-key\n")
            key.chmod(0o600)
            self.assertEqual(mod.secret_file(key), "test-key")
            symlink = Path(td) / "link"
            symlink.symlink_to(key)
            with self.assertRaises(mod.SmokeError):
                mod.secret_file(symlink)
            key.chmod(0o644)
            with self.assertRaises(mod.SmokeError):
                mod.secret_file(key)
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            state = mod.main(["--endpoint", "http://127.0.0.1:4000", "--alias", "coding",
                              "--workload-key-file", "/does/not/exist"])
        self.assertEqual(state, 0)
        self.assertIn("DRY RUN", output.getvalue())


if __name__ == "__main__":
    unittest.main()
