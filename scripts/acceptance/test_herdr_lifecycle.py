import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
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
        closed = False

        def record(machine, *args):
            nonlocal closed
            events.append(args)
            if args == ("workspace", "list"):
                workspaces = [] if closed else [
                    {"workspace_id": "w99", "label": self.identity},
                    {"workspace_id": "unrelated", "label": "leave-alone"},
                ]
                return json.dumps({"result": {
                    "type": "workspace_list", "workspaces": workspaces,
                }})
            if args == ("workspace", "close", "w99"):
                closed = True
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
        self.assertEqual(events[-3:], [
            ("workspace", "list"),
            ("workspace", "close", "w99"),
            ("workspace", "list"),
        ])

    def seed_owned(self):
        with patch.object(mod, "invoke", side_effect=self.fake):
            mod.phase("open", str(self.base), self.identity)
        self.assertTrue(mod.state_path(self.base).exists())

    def test_missing_or_wrong_ownership_in_inventory_blocks_close(self):
        self.seed_owned()
        for workspaces in (
            [],
            [{"workspace_id": "w99", "label": "other-run"}],
            [{"workspace_id": "w-other", "label": self.identity}],
        ):
            with self.subTest(workspaces=workspaces):
                listing = json.dumps({"result": {
                    "type": "workspace_list", "workspaces": workspaces,
                }})
                with patch.object(mod, "invoke", return_value=listing) as invoke, self.assertRaises(
                    RuntimeError
                ):
                    mod.phase("close", str(self.base), self.identity)
                invoke.assert_called_once_with("pilot", "workspace", "list")
                self.assertTrue(mod.state_path(self.base).exists())

    def test_zero_exit_from_close_is_not_remote_cleanup_proof(self):
        self.seed_owned()
        listing = json.dumps({"result": {
            "type": "workspace_list",
            "workspaces": [{"workspace_id": "w99", "label": self.identity}],
        }})
        calls = []

        def no_removal(machine, *args):
            calls.append(args)
            if args == ("workspace", "list"):
                return listing
            self.assertEqual(args, ("workspace", "close", "w99"))
            return "{}"

        with patch.object(mod, "invoke", side_effect=no_removal), self.assertRaises(
            RuntimeError
        ):
            mod.phase("close", str(self.base), self.identity)
        self.assertEqual(calls, [
            ("workspace", "list"),
            ("workspace", "close", "w99"),
            ("workspace", "list"),
        ])
        self.assertTrue(mod.state_path(self.base).exists())

    def test_invalid_inventory_is_never_empty_success(self):
        invalid = (
            "{}",
            "[]",
            '{"result":{"type":"workspace_list","workspaces":null}}',
            '{"result":{"type":"other","workspaces":[]}}',
            '{"result":{"type":"workspace_list","workspaces":[{}]}}',
            '{"result":{"type":"workspace_list","workspaces":[{"workspace_id":23,"label":"x"}]}}',
            '{"result":{"type":"workspace_list","workspaces":[42]}}',
            '{"result":{"type":"workspace_list","workspaces":[{"workspace_id":"w99","label":"a"},{"workspace_id":"w99","label":"b"}]}}',
            "{not JSON",
            "x" * 65537,
        )
        for payload in invalid:
            with self.subTest(payload=payload[:80]), patch.object(
                mod, "invoke", return_value=payload
            ), self.assertRaises(RuntimeError):
                mod.workspace_inventory("pilot")

    def test_unreadable_postclose_inventory_keeps_ownership_record(self):
        self.seed_owned()
        present = json.dumps({"result": {
            "type": "workspace_list",
            "workspaces": [{"workspace_id": "w99", "label": self.identity}],
        }})
        calls = []

        def missing_post(machine, *args):
            calls.append(args)
            if args == ("workspace", "list"):
                return present if len(calls) == 1 else "{}"
            return "{}"

        with patch.object(mod, "invoke", side_effect=missing_post), self.assertRaises(
            RuntimeError
        ):
            mod.phase("close", str(self.base), self.identity)
        self.assertEqual(len(calls), 3)
        self.assertTrue(mod.state_path(self.base).exists())

    def test_nonzero_close_cannot_remove_ownership_record(self):
        self.seed_owned()
        listing = json.dumps({"result": {
            "type": "workspace_list",
            "workspaces": [{"workspace_id": "w99", "label": self.identity}],
        }})
        def fails_close(machine, *args):
            if args == ("workspace", "list"):
                return listing
            raise RuntimeError("close command failed")

        with patch.object(mod, "invoke", side_effect=fails_close), self.assertRaises(
            RuntimeError
        ):
            mod.phase("close", str(self.base), self.identity)
        self.assertTrue(mod.state_path(self.base).exists())

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
        with patch.object(mod, "invoke", return_value='{"result":{}}'), self.assertRaises(RuntimeError):
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
