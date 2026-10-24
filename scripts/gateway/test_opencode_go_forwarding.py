"""No provider, gateway, or key is required to test the forwarding boundary."""
import asyncio
import copy
import importlib.util
import sys
import types
import unittest
from pathlib import Path
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "opencode_go_forwarding", Path(__file__).with_name("opencode_go_forwarding.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

SESSION_A = "ses_0000000000abcdefgh"
SESSION_B = "ses_1111111111abcdefgh"


def sample(session=SESSION_A, **extra):
    return {
        "model": mod.GO_ALIAS,
        "proxy_server_request": {"headers": {
            "x-opencode-session-id": session,
            "User-Agent": "opencode/1.0.0",
            "Authorization": "Bearer never-forward-me",
            "x-api-key": "do-not-forward",
            "x-litellm-metadata": "private-metadata",
            **extra,
        }},
        "headers": {"x-api-key": "previously-forwarded"},
        "extra_headers": {"x-bad": "body-controlled"},
        "input": "fixture prompt stays unchanged",
    }


class ForwardingTests(unittest.TestCase):
    def test_one_session_remains_stable_and_isolated_from_another(self):
        outbound = [mod.sanitize_go_headers(sample(s), "aresponses")["headers"]
                    for s in (SESSION_A, SESSION_A, SESSION_B)]
        self.assertEqual(outbound[0], outbound[1])
        self.assertNotEqual(outbound[0], outbound[2])
        self.assertEqual(outbound[0], {"x-opencode-session": SESSION_A,
                                       "User-Agent": "opencode/1.0.0"})

    def test_broad_forwarding_and_body_overrides_are_stripped(self):
        data = sample()
        updated = mod.sanitize_go_headers(data, "aresponses")
        self.assertEqual(set(updated["headers"]), {"x-opencode-session", "User-Agent"})
        self.assertNotIn("extra_headers", updated)
        self.assertEqual(updated["input"], "fixture prompt stays unchanged")
        self.assertEqual(updated["proxy_server_request"]["headers"]["Authorization"],
                         "Bearer never-forward-me")

    def test_accepts_both_native_headers_only_when_equal(self):
        self.assertEqual(mod.sanitize_go_headers(sample(**{
            "x-opencode-session": SESSION_A}), "aresponses")["headers"]["x-opencode-session"], SESSION_A)
        with self.assertRaisesRegex(ValueError, "disagree"):
            mod.sanitize_go_headers(sample(**{"x-opencode-session": SESSION_B}), "aresponses")

    def test_fails_closed_on_missing_malformed_oversized_or_conflicting_identity(self):
        bad = [
            {"x-opencode-session-id": ""},
            {"x-opencode-session-id": "fake"},
            {"x-opencode-session-id": "ses_" + "a" * 129},
            {"x-opencode-session-id": SESSION_A + "\r\nX-Api-Key: secret"},
            {"User-Agent": "python-urllib/3.13"},
            {"User-Agent": "opencode/1.0.0\r\nAuthorization: secret"},
            {"User-Agent": "opencode/" + "a" * 61},
        ]
        for changes in bad:
            with self.subTest(changes=tuple(changes)), self.assertRaises(ValueError):
                mod.sanitize_go_headers(sample(**changes), "aresponses")
        for invalid in ({}, {"proxy_server_request": {"headers": []}},
                        {"proxy_server_request": {"headers": {"User-Agent": "opencode/1.0"}}}):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                mod.sanitize_go_headers({"model": mod.GO_ALIAS, **invalid}, "aresponses")

    def test_unrelated_model_and_nonresponses_are_unmodified(self):
        for call_type, model in (("acompletion", mod.GO_ALIAS),
                                 ("aresponses", "other-model")):
            original = sample()
            original["model"] = model
            before = copy.deepcopy(original)
            self.assertIs(mod.sanitize_go_headers(original, call_type), original)
            self.assertEqual(before, original)

    def test_callback_implements_pinned_async_hook_contract(self):
        # Only replace LiteLLM's base class; the forwarding policy is real.
        logger = types.ModuleType("litellm.integrations.custom_logger")
        logger.CustomLogger = type("CustomLogger", (), {})
        root = types.ModuleType("litellm")
        root.__path__ = []
        integrations = types.ModuleType("litellm.integrations")
        integrations.__path__ = []
        with patch.dict(sys.modules, {"litellm": root, "litellm.integrations": integrations,
                                      "litellm.integrations.custom_logger": logger,
                                      "opencode_go_forwarding": mod}):
            adapter = importlib.util.spec_from_file_location(
                "litellm_opencode_go_hook", Path(__file__).with_name("litellm_opencode_go_hook.py"))
            hook = importlib.util.module_from_spec(adapter)
            adapter.loader.exec_module(hook)
            self.assertIsInstance(hook.proxy_handler_instance, logger.CustomLogger)
            data = sample()
            response = asyncio.run(hook.proxy_handler_instance.async_pre_call_hook(
                None, None, data, "aresponses"))
            self.assertEqual(response["headers"], {"x-opencode-session": SESSION_A,
                                                    "User-Agent": "opencode/1.0.0"})


if __name__ == "__main__":
    unittest.main()
