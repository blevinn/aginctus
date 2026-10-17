"""Offline contract cases for #122. No gateway or provider is contacted."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest

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
    def __init__(self, marker):
        self.marker = marker
        self.calls = []

    def open(self, req, timeout):
        self.calls.append(req)
        authorization = req.get_header("Authorization")
        if req.full_url.endswith("/v1/models") and authorization != "Bearer good-workload":
            raise mod.HTTPError(req.full_url, 401, "secret leaked in error", None, None)
        if req.full_url.endswith("/v1/models"):
            data = b'{"data": [{"id":"coding"}]}'
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
