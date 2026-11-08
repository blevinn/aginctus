"""Managed credential delivery is atomic, crash-recoverable and replay-safe."""

import hashlib
import json
import tempfile
import unittest
from pathlib import Path

import siwc


class PendingTests(unittest.TestCase):
    def test_publish_restart_and_reject_replay(self):
        with tempfile.TemporaryDirectory() as parent:
            store = siwc.Store(Path(parent) / "credentials")
            store.accept_pending()
            original = (store.root / "state.json").read_bytes()
            replacement = store.read()
            replacement["accounts"]["personal"] = {"client_id": "oaiapp_fixture", "scopes": []}
            pending = {"base_digest": hashlib.sha256(original).hexdigest(), "state": replacement}
            path = store.root / "pending.json"

            def stage():
                path.write_text(json.dumps(pending))
                path.chmod(0o600)

            stage()
            store.accept_pending()
            self.assertEqual(store.read(), replacement)
            self.assertFalse(path.exists())
            # Recover a crash after publication but before pending-file removal.
            stage()
            store.accept_pending()
            newer = store.read()
            newer["accounts"]["personal"]["scopes"] = ["openid"]
            store.write(newer)
            stage()
            with self.assertRaisesRegex(siwc.Error, "Stale"):
                store.accept_pending()
            self.assertEqual(store.read(), newer)

    def test_partial_delivery_preserves_authoritative_state(self):
        with tempfile.TemporaryDirectory() as parent:
            store = siwc.Store(Path(parent) / "credentials")
            store.accept_pending()
            original = store.read()
            path = store.root / "pending.json"
            path.write_text('{"state":')
            path.chmod(0o600)
            with self.assertRaises(siwc.Error):
                store.accept_pending()
            self.assertEqual(store.read(), original)


if __name__ == "__main__":
    unittest.main()
