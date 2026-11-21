"""LiteLLM-only refresh tests. Enrollment/admin behavior is tested in Go."""

import concurrent.futures
import hashlib
import json
import os
import tempfile
import time
import unittest
from pathlib import Path

import jwt
import siwc
from cryptography.hazmat.primitives.asymmetric import rsa


class FakeProvider:
    def __init__(self):
        self.key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        self.now = 1800000000
        self.scope = siwc.SCOPES
        self.client = "oaiapp_test"
        self.subject = "synthetic-subject"
        self.refreshes = 0
        self.error = None
        self.bad_claim = {}
        self.provider = siwc.Provider(self.request, lambda: self.now)

    def claims(self, audience, **extra):
        return {"iss": siwc.ISSUER, "aud": audience, "iat": self.now,
                "exp": self.now + 3600, "sub": self.subject, **extra, **self.bad_claim}

    def signed(self, claims):
        return jwt.encode(claims, self.key, algorithm="RS256", headers={"kid": "fixture"})

    def request(self, url, form=None, bearer=None):
        if url.endswith("openid-configuration"):
            return {"issuer": siwc.ISSUER, "jwks_uri": siwc.ISSUER + "/keys",
                    "revocation_endpoint": siwc.ISSUER + "/revoke"}
        if url.endswith("/keys"):
            key = json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(self.key.public_key()))
            return {"keys": [{**key, "kid": "fixture", "use": "sig", "alg": "RS256"}]}
        if url.endswith("/oauth/token"):
            if self.error:
                raise self.error
            assert form["client_id"] == self.client
            assert form["resource"] == siwc.RESOURCE
            assert form["grant_type"] == "refresh_token"
            assert "scope" not in form
            assert form["refresh_token"] == "synthetic-refresh-0"
            self.refreshes += 1
            time.sleep(0.02)
            return self.token_reply()
        raise AssertionError("Unexpected synthetic endpoint")

    def token_reply(self):
        return {"access_token": self.signed(self.claims(siwc.RESOURCE, client_id=self.client, scope=self.scope)),
                "refresh_token": "synthetic-refresh-" + str(self.refreshes),
                "id_token": self.signed(self.claims(self.client)),
                "scope": self.scope, "expires_in": 3600, "token_type": "Bearer"}

    def seed(self, store, bind=False):
        """Create synthetic runtime state, not a second enrollment implementation."""
        reply = self.token_reply()
        state = store.read()
        state["accounts"]["personal"] = {
            "client_id": self.client, "sub": self.subject, "access_token": reply["access_token"],
            "refresh_token": reply["refresh_token"], "id_token": reply["id_token"],
            "scopes": self.scope.split(), "expires_at": self.now + 3600, "earliest_refresh_at": 0}
        if bind:
            state["routes"]["siwc-coding"] = {
                "account": "personal", "client_id": self.client, "sub": self.subject, "model": "fixture-model",
                "key_hashes": [hashlib.sha256(b"sk-synthetic-workload").hexdigest()],
                "consent_background": True, "consent_connected_app": True}
        store.write(state)


class SIWCRuntimeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.store = siwc.Store(Path(self.temp.name) / "state")
        self.fake = FakeProvider()
        self.fake.seed(self.store)
        self.owner = siwc.Owner(self.store, self.fake.provider)

    def test_rotation_serializes_across_owners_and_survives_restart(self):
        self.fake.now += 3601
        def access(_):
            return siwc.Owner(siwc.Store(self.store.root), self.fake.provider).access("personal")
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            tokens = list(pool.map(access, range(8)))
        self.assertEqual(len(set(tokens)), 1)
        self.assertEqual(self.fake.refreshes, 1)
        self.assertEqual(self.store.read()["accounts"]["personal"]["refresh_token"], "synthetic-refresh-1")
        self.assertEqual(self.owner.access("personal"), tokens[0])

    def test_terminal_refresh_clears_tokens(self):
        self.fake.now += 3601
        self.fake.error = siwc.ProviderError(400, "refresh_token_reused")
        with self.assertRaises(siwc.ProviderError):
            self.owner.access("personal")
        record = self.store.read()["accounts"]["personal"]
        self.assertNotIn("access_token", record)
        self.assertEqual(record["client_id"], self.fake.client)

    def test_temporary_error_preserves_session_ambiguous_rotation_fails_closed(self):
        self.fake.now += 3601
        original = self.store.read()["accounts"]["personal"]
        self.fake.error = siwc.ProviderError(503)
        with self.assertRaises(siwc.ProviderError):
            self.owner.access("personal")
        self.assertEqual(original, self.store.read()["accounts"]["personal"])
        self.fake.error = siwc.Error("synthetic network ambiguity")
        with self.assertRaises(siwc.Error):
            self.owner.access("personal")
        self.assertTrue(self.store.read()["accounts"]["personal"]["refresh_pending"])
        with self.assertRaisesRegex(siwc.Error, "uncertain"):
            self.owner.access("personal")

    def test_invalid_replacement_never_overwrites_old_session(self):
        self.fake.now += 3601
        self.fake.bad_claim = {"aud": "another-client"}
        with self.assertRaises(siwc.Error):
            self.owner.access("personal")
        record = self.store.read()["accounts"]["personal"]
        self.assertEqual(record["refresh_token"], "synthetic-refresh-0")
        self.assertTrue(record["refresh_pending"])

    def test_plan_permission_and_refresh_window_fail_closed(self):
        state = self.store.read()
        state["accounts"]["personal"]["scopes"] = ["openid"]
        self.store.write(state)
        with self.assertRaises(siwc.PlanPermissionRequired):
            self.owner.access("personal")
        self.fake.seed(self.store)
        state = self.store.read()
        state["accounts"]["personal"]["earliest_refresh_at"] = self.fake.now + 7200
        self.store.write(state)
        self.fake.now += 3601
        with self.assertRaisesRegex(siwc.Error, "refresh window"):
            self.owner.access("personal")

    def test_private_state_rejects_modes_symlinks_hardlinks_and_fifo(self):
        path = self.store.root / "state.json"
        os.chmod(path, 0o644)
        with self.assertRaises(siwc.Error):
            self.store.read()
        os.chmod(path, 0o600)
        link = self.store.root / "link"
        os.link(path, link)
        with self.assertRaises(siwc.Error):
            self.store.read()
        link.unlink()
        path.unlink()
        path.symlink_to(link)
        with self.assertRaises(OSError):
            self.store.read()
        path.unlink()
        os.mkfifo(path, 0o600)
        with self.assertRaises(siwc.Error):
            self.store.read()

    def test_provider_redacts_errors_and_refuses_other_destinations(self):
        self.assertNotIn("secret", str(siwc.ProviderError(400, "secret")))
        with self.assertRaises(siwc.Error):
            siwc.request_json("http://auth.openai.com/token")


if __name__ == "__main__":
    unittest.main()
