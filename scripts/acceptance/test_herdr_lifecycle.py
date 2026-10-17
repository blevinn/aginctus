import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("herdr_lifecycle", Path(__file__).with_name("herdr-lifecycle.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.identity = "aginctus-acceptance.Ab12Cd34"
        self.base = Path(self.tmp.name) / self.identity / "workspace"
        self.base.mkdir(parents=True)
        self.env = patch.dict(os.environ, {
            "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "pilot",
            "AGINCTUS_ACCEPTANCE_GUEST_ROOT": "/home/agent/aginctus-acceptance/" + self.identity,
            "AGINCTUS_ACCEPTANCE_GUEST_STAGED": self.identity
        })
        self.env.start()
        self.addCleanup(self.env.stop)

    def fake(self, machine, *args):
        if args[:2] == ("workspace", "create"):
            return json.dumps({"result": {"workspace": {"workspace_id": "w99"},
                                          "root_pane": {"pane_id": "w99:p1"}}})
        return "{}"

    def test_lifecycle_owned_ids_and_cleanup(self):
        events = []
        def record(machine, *args):
            events.append(args)
            return self.fake(machine, *args)
        with patch.object(mod, "invoke", side_effect=record):
            mod.phase("open", str(self.base), self.identity)
            info = mod.load(self.base, self.identity)
            self.assertEqual(info["workspace_id"], "w99")
            self.assertEqual(info["pane_id"], "w99:p1")
            self.assertEqual(mod.state_path(self.base).stat().st_mode & 0o077, 0)
            mod.phase("start", str(self.base), self.identity)
            mod.phase("prompt", str(self.base), self.identity, "fix Add")
            mod.phase("wait", str(self.base), self.identity)
            mod.phase("stop", str(self.base), self.identity)
            mod.phase("close", str(self.base), self.identity)
        self.assertFalse(mod.state_path(self.base).exists())
        self.assertEqual(events[0][:2], ("workspace", "create"))
        self.assertEqual(events[1][:2], ("agent", "start"))
        self.assertEqual(events[2][:2], ("agent", "prompt"))
        self.assertEqual(events[-1], ("workspace", "close", "w99"))

    def test_rejects_unstaged_or_unsafe_workspace(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_GUEST_STAGED": ""}):
            with patch.object(mod, "invoke") as invoke, self.assertRaises(RuntimeError):
                mod.phase("open", str(self.base), self.identity)
            invoke.assert_not_called()
        with self.assertRaises(RuntimeError):
            mod.phase("open", str(self.base), "../dangerous")

    def test_refuses_close_without_ownership(self):
        with patch.object(mod, "invoke") as invoke, self.assertRaises(RuntimeError):
            mod.phase("close", str(self.base), self.identity)
        invoke.assert_not_called()

    def test_partial_create_without_valid_ids_fails_closed(self):
        with patch.object(mod, "invoke", return_value='{"result":{}}'):
            with self.assertRaises(RuntimeError):
                mod.phase("open", str(self.base), self.identity)
        self.assertFalse(mod.state_path(self.base).exists())

    def test_rejects_cross_machine_close(self):
        with patch.object(mod, "invoke", side_effect=self.fake):
            mod.phase("open", str(self.base), self.identity)
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "other"}):
            with patch.object(mod, "invoke") as invoke, self.assertRaises(RuntimeError):
                mod.phase("close", str(self.base), self.identity)
            invoke.assert_not_called()

if __name__ == "__main__":
    unittest.main()
