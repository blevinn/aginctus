"""Offline tests; never contacts a real provider or a running LiteLLM."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

TARGET = Path(__file__).with_name("register-openai-model.py")
spec = importlib.util.spec_from_file_location("register_openai_model", TARGET)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)


class Reply:
    def __init__(self, body, status=200):
        self.status = status
        self.body = json.dumps(body).encode()

    def __enter__(self):
        return self

    def __exit__(self, *_):
        pass

    def read(self, n):
        return self.body[:n]


class FakeOpener:
    def __init__(self, ids=(), failure_at=None):
        self.ids = list(ids)
        self.requests = []
        self.failure_at = failure_at

    def open(self, req, timeout):
        self.requests.append(req)
        n = len(self.requests)
        if n == self.failure_at:
            raise mod.URLError("upstream-secret-leak")
        if req.full_url.endswith("/v1/models"):
            return Reply({"data": [{"id": x} for x in self.ids]})
        if req.full_url.endswith("/model/new"):
            doc = json.loads(req.data)
            self.ids.append(doc["model_name"])
            return Reply({"model_name": doc["model_name"]})
        raise AssertionError(req.full_url)


class RegisterTests(unittest.TestCase):
    def test_private_endpoint_rejects_public_and_redirects(self):
        self.assertEqual(mod.private_endpoint("http://10.42.0.7:4000"), "http://10.42.0.7:4000")
        for value in ("https://10.42.0.7:4000", "http://1.1.1.1:4000",
                      "http://evil.example:4000", "http://10.42.0.7:8080",
                      "http://u:p@10.42.0.7:4000", "http://10.42.0.7:4000/fake",
                      "http://10.42.0.7:4000?next=https://evil", "http://[::1]:4000",
                      "http://10.42.0.7:bad"):
            with self.subTest(value=value), self.assertRaises(mod.RegistrationError):
                mod.private_endpoint(value)
        with self.assertRaises(mod.RegistrationError):
            mod.NoRedirect().redirect_request(None, None, 302, "redirect", {}, "https://example.com")

    def test_register_then_confirm_without_echoing_secret(self):
        fake = FakeOpener()
        mod.register("http://10.42.0.7:4000", "aginctus-coding", "openai/gpt-4o-mini",
                     "admin-unique", "provider-secret", fake)
        self.assertEqual(len(fake.requests), 3)
        self.assertEqual(fake.requests[1].get_method(), "POST")
        req = json.loads(fake.requests[1].data)
        self.assertEqual(req["litellm_params"]["api_key"], "provider-secret")
        self.assertEqual(req["model_info"]["mode"], "responses")
        self.assertEqual(fake.ids, ["aginctus-coding"])
        with self.assertRaisesRegex(mod.RegistrationError, "already exists"):
            mod.register("http://10.42.0.7:4000", "aginctus-coding", "openai/gpt-4o-mini",
                         "admin-unique", "provider-secret", fake)
        self.assertEqual(len(fake.requests), 4)

    def test_fails_closed_without_printing_provider_error(self):
        fake = FakeOpener(failure_at=2)
        with self.assertRaises(mod.RegistrationError) as cm:
            mod.register("http://10.42.0.7:4000", "a", "openai/x", "master", "provider-secret", fake)
        self.assertNotIn("upstream-secret-leak", str(cm.exception))
        self.assertNotIn("provider-secret", str(cm.exception))
        with self.assertRaises(mod.RegistrationError):
            mod.aliases({"data": [{"unexpected": "x"}]})

    def test_safe_credential_file_rejects_symlink_or_loose_permissions(self):
        with tempfile.TemporaryDirectory() as td:
            file = Path(td) / "key"
            file.write_text("secret\n")
            file.chmod(0o600)
            self.assertEqual(mod.credential(mod.protected_read(file)), "secret")
            link = Path(td) / "link"
            link.symlink_to(file)
            with self.assertRaises(mod.RegistrationError):
                mod.protected_read(link)
            file.chmod(0o644)
            with self.assertRaises(mod.RegistrationError):
                mod.protected_read(file)

    def test_dry_run_has_no_credentials_and_no_side_effect(self):
        out, err = io.StringIO(), io.StringIO()
        with patch.dict("os.environ", {"XDG_STATE_HOME": "/does/not/exist"}), \
                contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            status = mod.main(["--endpoint", "http://127.0.0.1:4000", "--alias", "coding",
                               "--source", "openai/gpt-4o-mini"])
        self.assertEqual(status, 0)
        self.assertIn("DRY RUN", out.getvalue())
        self.assertEqual(err.getvalue(), "")
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(err):
            status = mod.main(["--endpoint", "http://127.0.0.1:4000", "--alias", "coding",
                               "--source", "openai/gpt-4o-mini", "--apply"])
        self.assertEqual(status, 1)
        self.assertIn("--upstream-key-file", err.getvalue())

    def test_validates_aliases_and_sources_before_any_mutation(self):
        for alias, source in (("../../foo", "openai/gpt-4o-mini"),
                              ("coding", "chatgpt/experimental"), ("", "openai/valid")):
            with self.subTest(alias=alias, source=source):
                with contextlib.redirect_stderr(io.StringIO()):
                    status = mod.main(["--endpoint", "http://127.0.0.1:4000",
                                       "--alias", alias, "--source", source, "--apply"])
                self.assertEqual(status, 1)


if __name__ == "__main__":
    unittest.main()
