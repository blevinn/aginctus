"""No provider, gateway, or key is required to test the forwarding boundary."""
import asyncio
import copy
import importlib.util
import sys
import types
import unittest
from pathlib import Path
from unittest import mock

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
    def test_real_sdk_user_agent_is_preserved_without_allowing_arbitrary_suffixes(self):
        agent = "opencode/1.15.10 ai-sdk/provider-utils/4.0.19 runtime/bun/1.3.11"
        data = mod.sanitize_go_headers(sample(**{"User-Agent": agent}), "aresponses")
        self.assertEqual(data["headers"], {"x-opencode-session": SESSION_A, "User-Agent": agent})
        for invalid in (agent + " extra/1.0", agent + "\r\nX-Api-Key: secret",
                        agent.replace("runtime/bun", "runtime/python"),
                        agent.replace("4.0.19", "a" * 61),
                        agent.replace("opencode/1.15.10", "python/3.13"),
                        "opencode/1.15.10 ai-sdk/provider-utils/4.0.19"):
            with self.subTest(agent=invalid), self.assertRaises(ValueError):
                mod.sanitize_go_headers(sample(**{"User-Agent": invalid}), "aresponses")

    def test_callback_rejects_wrong_or_missing_runtime_at_import(self):
        import importlib.metadata

        for value in ("1.102.0", "1.103.0.post1", "1.103.00", "1.104.0", None):
            with self.subTest(version=value):
                adapter = importlib.util.spec_from_file_location(
                    "litellm_opencode_go_hook", Path(__file__).with_name("litellm_opencode_go_hook.py"))
                hook = importlib.util.module_from_spec(adapter)
                kwargs = ({"return_value": value} if value is not None else
                          {"side_effect": importlib.metadata.PackageNotFoundError("litellm")})
                with mock.patch("importlib.metadata.version", **kwargs) as version:
                    with self.assertRaises((RuntimeError, importlib.metadata.PackageNotFoundError)):
                        adapter.loader.exec_module(hook)
                    version.assert_called_once_with("litellm")

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

    def test_pinned_gateway_session_headers_must_match_native_open_code(self):
        # Explicit x-litellm-session-id has precedence over vendor-specific
        # session headers in pinned LiteLLM v1.103.0 spend attribution.
        good = sample(**{
            "x-opencode-session": SESSION_A,
            "x-litellm-session-id": SESSION_A,
        })
        output = mod.sanitize_go_headers(good, "aresponses")
        self.assertEqual(output["headers"], {
            "x-opencode-session": SESSION_A, "User-Agent": "opencode/1.0.0",
        })
        self.assertNotIn("x-litellm-session-id", output["headers"])
        self.assertNotIn("extra_headers", output)

        for header in ("x-litellm-session-id", "x-litellm-trace-id"):
            with self.subTest(header=header):
                original = sample(**{
                    "x-opencode-session": SESSION_A,
                    header: SESSION_B,
                })
                before = copy.deepcopy(original)
                with self.assertRaisesRegex(ValueError, "conflicting gateway trace"):
                    mod.sanitize_go_headers(original, "aresponses")
                # Fail before mutating anything or reaching the provider.
                self.assertEqual(original, before)

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

    def test_approved_alias_rejects_every_nonresponses_type_before_mutation(self):
        for call_type in ("acompletion", "completion", "atext_completion", "embedding", "", None):
            for original in (sample(), {"model": mod.GO_ALIAS}):
                before = copy.deepcopy(original)
                with self.subTest(call_type=call_type), self.assertRaisesRegex(
                    ValueError, "requires native Responses"
                ):
                    mod.sanitize_go_headers(original, call_type)
                self.assertEqual(before, original)

    def test_unrelated_model_is_unmodified_for_all_call_types(self):
        for call_type in ("aresponses", "acompletion", "completion", "atext_completion",
                          "embedding", "", None):
            original = sample()
            original["model"] = "other-model"
            before = copy.deepcopy(original)
            self.assertIs(mod.sanitize_go_headers(original, call_type), original)
            self.assertEqual(before, original)

    def test_callback_implements_pinned_async_hook_contract(self):
        # Only replace LiteLLM/FastAPI dependencies; forwarding policy is real.
        class FakeHTTPException(Exception):
            def __init__(self, status_code, detail):
                super().__init__(detail)
                self.status_code = status_code
                self.detail = detail

        fastapi = types.ModuleType("fastapi")
        fastapi.HTTPException = FakeHTTPException
        logger = types.ModuleType("litellm.integrations.custom_logger")
        logger.CustomLogger = type("CustomLogger", (), {})
        root = types.ModuleType("litellm")
        root.__path__ = []
        integrations = types.ModuleType("litellm.integrations")
        integrations.__path__ = []
        with mock.patch("importlib.metadata.version", return_value="1.103.0"), mock.patch.dict(sys.modules, {"litellm": root, "litellm.integrations": integrations,
                                      "litellm.integrations.custom_logger": logger,
                                      "opencode_go_forwarding": mod,
                                      "fastapi": fastapi}):
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

            # The pinned proxy maps HTTPException to client 4xx; bare ValueError
            # surfaced as HTTP 500 in the operator's actual pinned OCI run.
            for changes in (
                {"x-opencode-session-id": "fake"},
                {"User-Agent": "python-httpx/0.28"},
                {"x-opencode-session-id": "ses_abcdefgh\r\nAuthorization: secret"},
            ):
                with self.subTest(changes=tuple(changes)), self.assertRaises(
                    FakeHTTPException
                ) as rejected:
                    asyncio.run(hook.proxy_handler_instance.async_pre_call_hook(
                        None, None, sample(**changes), "aresponses"))
                self.assertEqual(rejected.exception.status_code, 400)
                self.assertEqual(
                    rejected.exception.detail, "Invalid OpenCode Go session metadata"
                )
                self.assertNotIn("secret", str(rejected.exception))

            with self.assertRaises(FakeHTTPException) as rejected:
                asyncio.run(hook.proxy_handler_instance.async_pre_call_hook(
                    None, None, sample(), "acompletion"))
            self.assertEqual(rejected.exception.status_code, 400)
            self.assertEqual(rejected.exception.detail, "Invalid OpenCode Go session metadata")

            unrelated = sample()
            unrelated["model"] = "other-model"
            result = asyncio.run(hook.proxy_handler_instance.async_pre_call_hook(
                None, None, unrelated, "aresponses"))
            self.assertIs(result, unrelated)


if __name__ == "__main__":
    unittest.main()
