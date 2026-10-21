"""Tests for the shared gateway endpoint validator."""
import importlib.util
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location(
    "gateway_endpoint", Path(__file__).with_name("gateway_endpoint.py")
)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)


class EndpointValidatorTests(unittest.TestCase):
    def test_accepts_canonical_http_v1_endpoint(self):
        url = mod.validate_endpoint("http://10.42.40.12:4000/v1")
        self.assertEqual(url.scheme, "http")
        self.assertEqual(url.hostname, "10.42.40.12")
        self.assertEqual(url.path, "/v1")

    def test_accepts_trailing_slash(self):
        # provision previously required the path to equal "/v1" exactly;
        # launch accepted a trailing slash. The shared validator keeps the
        # more permissive launch behavior.
        mod.validate_endpoint("http://10.42.40.12:4000/v1/")

    def test_rejects_https(self):
        with self.assertRaises(ValueError):
            mod.validate_endpoint("https://example.com/v1")

    def test_rejects_username_userinfo(self):
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http://user@example.com/v1")

    def test_rejects_password_userinfo(self):
        # provision previously allowed this because the check was
        # ``url.username:`` truthiness on "". The shared validator
        # rejects either username or password, matching launch's stricter
        # rule and the Go CLI's contract.
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http://:secret@example.com/v1")

    def test_rejects_query_and_fragment(self):
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http://10.42.40.12:4000/v1?token=x")
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http://10.42.40.12:4000/v1#frag")

    def test_rejects_non_v1_path(self):
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http://10.42.40.12:4000/v2")
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http://10.42.40.12:4000/")
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http://10.42.40.12:4000")

    def test_rejects_whitespace_including_newline(self):
        # urlsplit strips \n and \t silently; the explicit pre-check makes
        # the rejection visible and consistent between provision and
        # launch.
        for bad in ("  http://10.42.40.12:4000/v1",
                    "http://10.42.40.12:4000/v1\n",
                    "http://10.42.40.12:4000/v1\t",
                    "http://10.42.40.12:4000 /v1"):
            with self.assertRaises(ValueError, msg=repr(bad)):
                mod.validate_endpoint(bad)

    def test_rejects_empty_or_missing_host(self):
        with self.assertRaises(ValueError):
            mod.validate_endpoint("http:///v1")

    def test_rejects_non_string(self):
        with self.assertRaises(ValueError):
            mod.validate_endpoint(None)
        with self.assertRaises(ValueError):
            mod.validate_endpoint(12345)


class ModelValidatorTests(unittest.TestCase):
    def test_accepts_simple_alias(self):
        self.assertEqual(mod.validate_model("gpt-6-sol"), "gpt-6-sol")

    def test_rejects_empty(self):
        with self.assertRaises(ValueError):
            mod.validate_model("")

    def test_rejects_whitespace_padded(self):
        with self.assertRaises(ValueError):
            mod.validate_model(" gpt-6-sol")
        with self.assertRaises(ValueError):
            mod.validate_model("gpt-6-sol\n")

    def test_rejects_slash(self):
        with self.assertRaises(ValueError):
            mod.validate_model("other/provider")

    def test_rejects_non_string(self):
        with self.assertRaises(ValueError):
            mod.validate_model(None)


class KeyValidatorTests(unittest.TestCase):
    def test_accepts_safe_key(self):
        self.assertEqual(mod.validate_key("sk-abc"), "sk-abc")

    def test_rejects_empty(self):
        with self.assertRaises(ValueError):
            mod.validate_key("")

    def test_rejects_whitespace_padded(self):
        with self.assertRaises(ValueError):
            mod.validate_key(" sk-abc")

    def test_rejects_embedded_newline(self):
        with self.assertRaises(ValueError):
            mod.validate_key("sk-a\nb")

    def test_rejects_embedded_cr(self):
        with self.assertRaises(ValueError):
            mod.validate_key("sk-a\rb")

    def test_rejects_non_string(self):
        with self.assertRaises(ValueError):
            mod.validate_key(None)


if __name__ == "__main__":
    unittest.main()