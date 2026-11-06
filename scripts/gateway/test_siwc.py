import base64
import concurrent.futures
import hashlib
import io
import json
import os
import tempfile
import threading
import time
import unittest
import urllib.parse
import urllib.request
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import jwt
import siwc
from cryptography.hazmat.primitives.asymmetric import rsa


class FakeProvider:
    def __init__(self):
        self.key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        self.now = 1800000000
        self.nonce = None
        self.verifier_hash = None
        self.scope = siwc.SCOPES
        self.client = "oaiapp_test"
        self.subject = "synthetic-subject"
        self.refreshes = 0
        self.error = None
        self.bad_claim = {}
        self.calls = []
        self.provider = siwc.Provider(self.request, lambda: self.now)

    def claims(self, audience, **extra):
        return {"iss": siwc.ISSUER, "aud": audience, "iat": self.now,
                "exp": self.now + 3600, "sub": self.subject, **extra, **self.bad_claim}

    def signed(self, claims):
        return jwt.encode(claims, self.key, algorithm="RS256", headers={"kid": "fixture"})

    def request(self, url, form=None, bearer=None):
        self.calls.append((url, form, bearer))
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
            if form["grant_type"] == "refresh_token":
                self.refreshes += 1
                assert "scope" not in form
                assert form["refresh_token"] == "synthetic-refresh-0"
                time.sleep(0.02)
            else:
                digest = hashlib.sha256(form["code_verifier"].encode()).digest()
                assert base64.urlsafe_b64encode(digest).decode().rstrip("=") == self.verifier_hash
                assert form["redirect_uri"] == self.redirect
            return {"access_token": self.signed(self.claims(siwc.RESOURCE, client_id=self.client, scope=self.scope)),
                    "refresh_token": "synthetic-refresh-" + str(self.refreshes),
                    "id_token": self.signed(self.claims(self.client, nonce=self.nonce)),
                    "scope": self.scope, "expires_in": 3600, "token_type": "Bearer"}
        if url.endswith("/models"):
            assert bearer
            return {"models": [{"slug": "fixture-model", "display_name": "Fixture", "visibility": "list"},
                               {"slug": "hidden", "visibility": "hide"}]}
        if url.endswith("/revoke"):
            if self.error:
                raise self.error
            assert form["client_id"] == self.client
            return {}
        raise AssertionError("Unexpected synthetic endpoint")

    def browser(self, address, transform=None):
        params = urllib.parse.parse_qs(urllib.parse.urlsplit(address).query)
        self.nonce = params["nonce"][0]
        self.verifier_hash = params["code_challenge"][0]
        self.redirect = params["redirect_uri"][0]
        self.auth_params = params
        values = {"state": params["state"][0], "code": "synthetic-code", "client_id": self.client}
        if transform:
            values = transform(values)
        callback = self.redirect + "?" + urllib.parse.urlencode(values, doseq=True)

        def send():
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            try:
                with opener.open(callback, timeout=2):
                    pass
            except OSError:
                pass
        threading.Thread(target=send, daemon=True).start()


class SIWCTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.store = siwc.Store(Path(self.temp.name) / "source")
        self.fake = FakeProvider()
        self.owner = siwc.Owner(self.store, self.fake.provider)
        self.output = io.StringIO()

    def login(self, transform=None):
        siwc.login(self.owner, "personal", lambda address: self.fake.browser(address, transform),
                   output=self.output, timeout=0.5)

    def test_authorization_persistence_restart_and_model_catalog(self):
        self.login()
        state = self.store.read()
        record = state["accounts"]["personal"]
        self.assertEqual(record["client_id"], self.fake.client)
        self.assertEqual(record["sub"], self.fake.subject)
        self.assertEqual(os.stat(self.store.root / "state.json").st_mode & 0o777, 0o600)
        restarted = siwc.Owner(siwc.Store(self.store.root), self.fake.provider)
        self.assertEqual(restarted.access("personal"), record["access_token"])
        self.assertEqual(restarted.models("personal"), [{"slug": "fixture-model", "display_name": "Fixture"}])
        self.assertEqual(self.fake.auth_params["client_id"], ["dynamic_agent_client"])
        self.assertEqual(self.fake.auth_params["agent_name_hint"], ["Aginctus"])
        self.assertNotIn(record["access_token"], self.output.getvalue())
        host = state["host_id"]
        self.login()
        self.assertEqual(self.fake.auth_params["client_id"], [self.fake.client])
        self.assertNotIn("agent_name_hint", self.fake.auth_params)
        self.assertNotIn("id_token_hint", self.fake.auth_params)
        self.assertEqual(self.store.read()["host_id"], host)

    def test_callbacks_reject_state_denial_missing_code_client_duplicates_and_timeout(self):
        transforms = [lambda v: {**v, "state": "bad"}, lambda v: {**v, "error": "access_denied"},
                      lambda v: {**v, "code": ""}, lambda v: {**v, "client_id": ""},
                      lambda v: {**v, "state": [v["state"], v["state"]]},
                      lambda v: {**v, "state": "nonascii-\u2603"}]
        for transform in transforms:
            with self.subTest(transform=transform), self.assertRaises(siwc.Error):
                self.login(transform)
            self.assertNotIn("personal", self.store.read()["accounts"])
        with self.assertRaisesRegex(siwc.Error, "timed out"):
            siwc.login(self.owner, "personal", lambda address: None, output=self.output, timeout=0.01)

    def test_exchange_error_retains_issued_registration_but_not_credentials(self):
        self.fake.error = siwc.ProviderError(400, "invalid_grant")
        with self.assertRaises(siwc.ProviderError):
            self.login()
        self.assertEqual(self.store.read()["accounts"]["personal"], {"client_id": self.fake.client})

    def test_identity_issuer_audience_nonce_signature_expiry_account_validation(self):
        for bad in ({"iss": "https://evil.invalid"}, {"aud": "other"}, {"nonce": "wrong"},
                    {"exp": 1}, {"iat": self.fake.now + 3600}, {"sub": ""}, {"nbf": self.fake.now + 3600}):
            with self.subTest(bad=bad):
                self.fake.bad_claim = bad
                with self.assertRaises(siwc.Error):
                    self.login()
                self.assertNotIn("access_token", self.store.read()["accounts"]["personal"])
        self.fake.bad_claim = {}
        self.login()
        original = self.store.read()["accounts"]["personal"]
        self.fake.subject = "different-account"
        with self.assertRaises(siwc.Error):
            self.login()
        self.assertEqual(self.store.read()["accounts"]["personal"], original)
        signed = self.fake.signed(self.fake.claims(self.fake.client))
        other = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        self.fake.key = other
        with self.assertRaises(siwc.Error):
            self.fake.provider.claims(signed, self.fake.client)

    def test_identity_without_plan_scope_is_saved_but_cannot_invoke(self):
        self.fake.scope = "openid profile email"
        self.login()
        self.assertEqual(self.store.read()["accounts"]["personal"]["sub"], self.fake.subject)
        with self.assertRaisesRegex(siwc.Error, "not granted"):
            self.owner.access("personal")

    def test_expiry_refresh_rotation_concurrent_owners_and_restart(self):
        self.login()
        self.fake.now += 3601
        def access(_):
            return siwc.Owner(siwc.Store(self.store.root), self.fake.provider).access("personal")
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            tokens = list(pool.map(access, range(8)))
        self.assertEqual(len(set(tokens)), 1)
        self.assertEqual(self.fake.refreshes, 1)
        persisted = self.store.read()["accounts"]["personal"]
        self.assertEqual(persisted["refresh_token"], "synthetic-refresh-1")
        self.assertNotIn("refresh_pending", persisted)
        self.assertEqual(self.owner.access("personal"), tokens[0])

    def test_revoked_refresh_clears_tokens_preserves_registration(self):
        self.login()
        self.fake.now += 3601
        self.fake.error = siwc.ProviderError(400, "refresh_token_reused")
        with self.assertRaises(siwc.ProviderError):
            self.owner.access("personal")
        record = self.store.read()["accounts"]["personal"]
        self.assertNotIn("access_token", record)
        self.assertEqual(record["client_id"], self.fake.client)

    def test_temporary_failure_preserves_credentials_ambiguous_rotation_fails_closed(self):
        self.login()
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
        self.fake.error = None
        self.login()
        self.assertNotIn("refresh_pending", self.store.read()["accounts"]["personal"])

    def test_logout_revocation_and_remote_failure_always_remove_local_tokens(self):
        self.login()
        self.assertTrue(self.owner.logout("personal"))
        self.assertNotIn("refresh_token", self.store.read()["accounts"]["personal"])
        self.login()
        self.fake.error = siwc.ProviderError(503)
        with patch("siwc.time.sleep"):
            self.assertFalse(self.owner.logout("personal"))
        self.assertNotIn("access_token", self.store.read()["accounts"]["personal"])

    def test_runtime_lease_blocks_enrollment_logout_and_transfer(self):
        self.login()
        with self.store.lock("runtime", timeout=0):
            with self.assertRaisesRegex(siwc.Error, "owner busy"):
                self.owner.logout("personal")
            with self.assertRaisesRegex(siwc.Error, "owner busy"):
                self.login()

    def test_private_store_rejects_symlinks_modes_fifo_hardlinks_and_corruption(self):
        self.login()
        target = self.store.root / "state.json"
        original = target.read_bytes()
        os.chmod(target, 0o644)
        with self.assertRaises(siwc.Error):
            self.store.read()
        os.chmod(target, 0o600)
        os.link(target, self.store.root / "hardlink")
        with self.assertRaises(siwc.Error):
            self.store.read()
        os.unlink(self.store.root / "hardlink")
        target.unlink()
        target.symlink_to(self.store.root / "absent")
        with self.assertRaises(OSError):
            self.store.read()
        with self.assertRaises(OSError):
            self.store.write({})
        target.unlink()
        target.write_bytes(b"not-json")
        os.chmod(target, 0o600)
        with self.assertRaises(siwc.Error):
            self.store.read()
        target.write_bytes(original)
        target.unlink()
        os.mkfifo(target, 0o600)
        with self.assertRaises(siwc.Error):
            self.store.read()
        with self.assertRaises(siwc.Error):
            siwc.Store(self.store.root / "..")

    def test_invalid_refresh_replacement_never_overwrites_the_old_session(self):
        self.login()
        self.fake.now += 3601
        original = self.store.read()["accounts"]["personal"]
        self.fake.bad_claim = {"aud": "another-client"}
        with self.assertRaises(siwc.Error):
            self.owner.access("personal")
        persisted = self.store.read()["accounts"]["personal"]
        self.assertEqual(persisted["refresh_token"], original["refresh_token"])
        self.assertTrue(persisted["refresh_pending"])

    def test_bind_consent_catalog_account_isolation_and_exclusive_handoff(self):
        self.login()
        key = self.store.root / "workload-key"
        key.write_text("sk-synthetic-workload")
        key.chmod(0o600)
        args = SimpleNamespace(route="siwc-coding", account="personal", model="fixture-model",
                               workload_key_file=str(key), consent_connected_app=False, consent_background=True)
        with self.assertRaises(siwc.Error):
            siwc.configure(self.owner, args)
        args.consent_connected_app = True
        siwc.configure(self.owner, args)
        args.model = "hidden"
        with self.assertRaises(siwc.Error):
            siwc.configure(self.owner, args)
        target = siwc.Store(Path(self.temp.name) / "target")
        destination = target.read()
        target.write(destination)
        capsule = self.store.root / "handoff.json"
        self.owner.export("personal", destination["host_id"], capsule)
        with self.assertRaises(siwc.Error):
            self.owner.access("personal")
        recipient = siwc.Owner(target, self.fake.provider)
        recipient.receive(capsule)
        self.assertEqual(recipient.models("personal")[0]["slug"], "fixture-model")
        with self.assertRaises(siwc.Error):
            recipient.receive(capsule)
        self.assertEqual(target.read()["host_id"], destination["host_id"])
        self.assertIn("siwc-coding", target.read()["routes"])

    def test_provider_redacts_echoed_secret_and_disallows_discovery_redirect(self):
        error = siwc.ProviderError(400, "synthetic-secret-value")
        self.assertNotIn("synthetic-secret", str(error))
        fake = siwc.Provider(lambda url: {"issuer": siwc.ISSUER,
                             "jwks_uri": "https://evil.invalid/keys", "revocation_endpoint": siwc.ISSUER + "/revoke"})
        with self.assertRaises(siwc.Error):
            fake.discovery()
        with self.assertRaises(siwc.Error):
            siwc.request_json("http://auth.openai.com/token")


if __name__ == "__main__":
    unittest.main()
