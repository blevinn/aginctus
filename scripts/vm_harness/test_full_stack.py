"""Hermetic fail-closed tests; no VM, Incus daemon or provider is contacted."""
import contextlib
import copy
import io
import ipaddress
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from scripts.vm_harness import full_stack as stack
from scripts.vm_harness import runner
from scripts.vm_harness.tool_sequence import SequenceError, ToolSequence


class FakeAdapter:
    base = Path("/tmp/unused/workspace")
    identity = "aginctus-acceptance.12345678"
    nonce = "12345678"

    def __init__(self, fail=None):
        self.fail = fail
        self.calls = []

    def phase(self, phase, workspace, identity, prompt=None):
        self.calls.append(phase)
        assert workspace == str(self.base) and identity == self.identity
        if phase == "prompt":
            assert "nonce=" + self.nonce in prompt
        if phase == self.fail:
            raise stack.gateway.Failure("injected missing evidence")

    def remove_fixture(self):
        self.calls.append("remove-fixture")

    def inspect_foreign(self):
        self.calls.append("inspect-foreign")
        if self.fail == "inspect-foreign":
            raise stack.gateway.Failure("foreign sentinel disappeared")


class FullStackTests(unittest.TestCase):
    def test_embedded_guest_shell_programs_parse_without_execution(self):
        # Python linters cannot see shell syntax inside the adapter's strings.
        import ast
        source = Path(stack.__file__).read_text()
        for node in ast.walk(ast.parse(source)):
            if (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                    and node.func.id in ("guest", "command") and len(node.args) >= 4
                    and isinstance(node.args[0], ast.Constant) and node.args[0].value == "sh"):
                script = ast.literal_eval(node.args[3])
                subprocess.run(["bash", "-n"], input=script, text=True,
                               capture_output=True, timeout=5, check=True)

    def test_real_phase_contract_and_scoped_cleanup(self):
        adapter = FakeAdapter()
        stack.exercise(adapter)
        self.assertEqual(adapter.calls, [*stack.PHASES, "remove-fixture", "inspect-foreign"])

    def test_each_gate_failure_fails_and_cleans_partial_work(self):
        for phase in stack.PHASES:
            with self.subTest(phase=phase):
                adapter = FakeAdapter(phase)
                with self.assertRaises(stack.gateway.Failure):
                    stack.exercise(adapter)
                self.assertEqual(adapter.calls[-2:], ["remove-fixture", "inspect-foreign"])
                if phase != "preflight":
                    self.assertIn("close", adapter.calls)
                if phase not in ("preflight", "open"):
                    self.assertIn("stop", adapter.calls)

    def test_remove_failure_is_not_success(self):
        adapter = FakeAdapter()
        adapter.remove_fixture = Mock(side_effect=stack.gateway.Failure("not absent"))
        with self.assertRaises(stack.gateway.Failure):
            stack.exercise(adapter)
        self.assertEqual(adapter.calls[-3:], ["stop", "close", "inspect-foreign"])

    def test_fixed_runtime_fault_closes_only_owned_workspace_and_inspects_foreign(self):
        adapter = FakeAdapter()
        with self.assertRaises(stack.InjectedFailure):
            stack.exercise(adapter, fail_after_open=True)
        self.assertEqual(adapter.calls, ["preflight", "open", "close", "remove-fixture", "inspect-foreign"])
        with self.assertRaises(stack.gateway.Failure):
            stack.exercise(FakeAdapter("inspect-foreign"))

    def test_foreign_sentinel_requires_independent_creation_and_unchanged_inventory(self):
        before = {"existing": "unrelated-user-work"}
        after = before | {"sentinel": "vm-foreign-sentinel-" + "a" * 16}
        inventory = Mock(side_effect=[before, after, after, before])
        invoke = Mock(return_value=json.dumps({"result": {"workspace": {"workspace_id": "sentinel"}}}))
        guard = stack.ForeignWorkspaceGuard({"workspace_inventory": inventory, "invoke": invoke})
        with patch.object(stack.secrets, "token_hex", return_value="a" * 16):
            guard.establish()
        guard.inspect()
        with self.assertRaisesRegex(stack.gateway.Failure, "unrelated Herdr inventory changed"):
            guard.inspect()
        self.assertEqual(invoke.call_count, 1)
        self.assertEqual(invoke.call_args.args[1:3], ("workspace", "create"))

    def test_credential_gate_supplies_actual_secrets_only_to_guest_root_stdin(self):
        adapter = stack.Adapter.__new__(stack.Adapter)
        adapter.base, adapter.identity = FakeAdapter.base, FakeAdapter.identity
        adapter.master, adapter.scoped = "actual-master-selector", "actual-scoped-selector"
        adapter.endpoint = "http://10.77.0.2:4000"
        with (patch.object(stack, "guest") as guest,
              patch.object(stack.gateway, "request", return_value=json.dumps({"data": [{"id": stack.ALIAS}]})),
              patch.object(stack, "denied") as denied,
              contextlib.redirect_stdout(io.StringIO())):
            adapter.phase("credential-check", str(adapter.base), adapter.identity)
        audit = guest.call_args_list[-1]
        self.assertEqual(audit.args, ("aginctus-vm-guest-credentials", "audit"))
        self.assertIs(audit.kwargs["agent"], False)
        payload = json.loads(audit.kwargs["data"])
        self.assertEqual(payload["master"], adapter.master)
        self.assertEqual(payload["provider"], stack.gateway.TOKEN)
        self.assertEqual(payload["scoped"], adapter.scoped)
        self.assertNotIn(adapter.master, repr(audit.args))
        self.assertNotIn(stack.gateway.TOKEN, repr(audit.args))
        self.assertEqual(denied.call_count, 3)

    def test_scoped_close_cannot_delete_a_baseline_workspace_even_with_our_label(self):
        for recorded in (True, False):
            with self.subTest(recorded=recorded):
                adapter = stack.Adapter.__new__(stack.Adapter)
                adapter.base, adapter.identity = FakeAdapter.base, FakeAdapter.identity
                adapter.guard = Mock(baseline={"foreign": adapter.identity})
                path = Mock()
                path.exists.return_value = recorded
                invoke, phase = Mock(), Mock()
                adapter.lifecycle = {
                    "state_path": Mock(return_value=path),
                    "load": Mock(return_value={"workspace_id": "foreign"}),
                    "workspace_inventory": Mock(return_value={"foreign": adapter.identity}),
                    "invoke": invoke, "phase": phase,
                }
                with self.assertRaises(stack.gateway.Failure):
                    adapter.phase("close", str(adapter.base), adapter.identity)
                invoke.assert_not_called()
                phase.assert_not_called()

    def test_exact_diff_and_actual_tool_marker_required(self):
        files = dict(stack.FILES)
        files["calc.go"] = files["calc.go"].replace("a - b", "a + b")
        files[".aginctus-tool-test-ok"] = "12345678"
        stack.verify_files(files, "12345678")
        for name in files:
            with self.subTest(name=name):
                bad = dict(files)
                bad[name] += "fabricated"
                with self.assertRaises(stack.gateway.Failure):
                    stack.verify_files(bad, "12345678")

    def test_gateway_records_require_fresh_key_alias_session_and_four_turns(self):
        rows = [{"request_id": "req_" + str(i), "session_id": "ses_12345678",
                 "api_key": "a" * 64, "model_group": stack.ALIAS,
                 "call_type": "aresponses", "status": "success"} for i in range(4)]
        stack.verify_rows(rows, "a" * 64, "ses_12345678")
        for field in rows[0]:
            for value in (None, "wrong"):
                bad = copy.deepcopy(rows)
                bad[0][field] = value
                # Request IDs are opaque, but must be present and unique.
                if field == "request_id" and value == "wrong":
                    continue
                with self.subTest(field=field, value=value), self.assertRaises(stack.gateway.Failure):
                    stack.verify_rows(bad, "a" * 64, "ses_12345678")
        for bad in ([], rows[:3], rows + rows[:1], [rows[0]] * 4, {}, None):
            with self.assertRaises(stack.gateway.Failure):
                stack.verify_rows(bad, "a" * 64, "ses_12345678")

    def test_native_transcript_is_required_not_just_provider_continuations(self):
        session, remote, nonce = "ses_12345678", "/workspace", "12345678"
        parts = [{"type": "tool", "tool": name, "callID": "call_" + nonce + "_" + str(i),
                  "state": {"status": "completed", "input": stack.tool_arguments(i, remote, nonce)}}
                 for i, name in enumerate(stack.STEP_NAMES)]
        parts += [{"type": "text", "text": "synthetic-tools-complete:" + nonce},
                  {"type": "step-finish", "reason": "stop"}]
        document = {"info": {"id": session, "directory": remote},
                    "messages": [{"info": {"role": "assistant"}, "parts": parts}]}
        stack.verify_transcript(document, session, remote, nonce)
        for index in range(len(parts)):
            bad = copy.deepcopy(document)
            bad["messages"][0]["parts"].pop(index)
            with self.assertRaises(stack.gateway.Failure):
                stack.verify_transcript(bad, session, remote, nonce)
        for field, value in (("callID", "forged"), ("tool", "write"),
                             ("state", {"status": "error"})):
            bad = copy.deepcopy(document)
            bad["messages"][0]["parts"][0][field] = value
            with self.assertRaises(stack.gateway.Failure):
                stack.verify_transcript(bad, session, remote, nonce)

    def test_only_auth_denials_count(self):
        for code in (401, 403):
            with patch.object(stack.gateway, "request", side_effect=stack.gateway.Failure(
                    f"synthetic gateway operation HTTP {code}")):
                stack.denied("unused", "/v1/models", None)
        for error in (None, "synthetic gateway operation HTTP 500", "synthetic gateway not reachable"):
            with (patch.object(stack.gateway, "request", side_effect=stack.gateway.Failure(error) if error else None),
                  self.assertRaises(stack.gateway.Failure)):
                stack.denied("unused", "/v1/models", None)

    def test_fixture_path_is_trusted_not_an_http_selected_tool_root(self):
        ToolSequence("/home/agent/aginctus-acceptance/aginctus-acceptance.12345678")
        for path in ("/home/agent", "/workspace/../etc", "/home/agent/aginctus-acceptance/foreign"):
            with self.assertRaises(SequenceError):
                ToolSequence(path)

    def test_entrypoint_refuses_checkout_without_any_mutation(self):
        with patch.object(stack, "command") as cmd, patch.object(stack.sys, "argv", ["full_stack.py"]):
            with self.assertRaises(stack.gateway.Failure):
                stack.run()
            cmd.assert_not_called()

    def test_real_cleanup_control_flow_revokes_key_and_model_even_after_task_failure(self):
        calls = []
        def request(_endpoint, path, token=None, body=None):
            calls.append(path)
            if path == "/v1/models" and token == "scoped-only":
                raise stack.gateway.Failure("synthetic gateway operation HTTP 401")
            if path == "/v1/models":
                present = "/model/new" in calls and "/model/delete" not in calls
                return json.dumps({"data": [{"id": stack.ALIAS}] if present else []}).encode()
            if path == "/model/new":
                return b'{"model_info":{"id":"owned-model"}}'
            if path == "/key/generate":
                return b'{"key":"scoped-only"}'
            return b'{}'
        state = Mock()
        state.is_symlink.return_value = False
        state.stat.return_value.st_mode = 0o600
        state.read_text.return_value = '{"AGINCTUS_GATEWAY_MASTER_KEY":"admin-only"}'
        thread = Mock()
        thread.is_alive.return_value = False
        server = Mock()
        with (patch.object(stack.sys, "argv", ["full_stack.py"]),
              patch.object(stack.Path, "resolve", return_value=Path("/nix/store/fixture/full_stack.py")),
              patch.object(stack.Path, "is_file", return_value=True),
              patch.object(stack.gateway, "STATE", state),
              patch.object(stack.gateway, "network_ipv4", side_effect=[
                  ipaddress.IPv4Interface("10.77.0.1/24"), ipaddress.IPv4Interface("10.78.0.1/24")]),
              patch.object(stack.gateway, "lite_ipv4", return_value="10.77.0.2"),
              patch.object(stack.gateway, "request", side_effect=request),
              patch.object(stack, "SyntheticServer", return_value=server),
              patch.object(stack.threading, "Thread", return_value=thread),
              patch.object(stack, "ForeignWorkspaceGuard"),
              patch.object(stack, "guest") as guest,
              patch.object(stack, "Adapter"),
              patch.object(stack, "exercise", side_effect=stack.gateway.Failure("task failed")),
              self.assertRaisesRegex(stack.gateway.Failure, "task failed")):
            stack.run()
        self.assertLess(calls.index("/key/delete"), calls.index("/model/delete"))
        self.assertEqual(calls[-1], "/v1/models")
        server.shutdown.assert_called_once()
        server.server_close.assert_called_once()
        self.assertEqual([call.args for call in guest.call_args_list], [
            ("aginctus-vm-guest-credentials", "remove-key"),
            ("aginctus-vm-guest-credentials", "inspect-key-absent"),
        ])
        self.assertEqual(json.loads(guest.call_args_list[0].kwargs["data"]), {"scoped": "scoped-only"})

    def test_full_stack_driver_is_fixed_and_reporting_stays_synthetic(self):
        self.assertEqual(runner.build_arguments("full-stack", False)[-1], ".#vm-harness-full-stack.driver")
        with tempfile.TemporaryDirectory() as temp:
            for code in (0, 1):
                report = runner.write_report("full-stack", code, runner.Provenance(), Path(temp))
                record = json.loads(report.read_text())
                self.assertEqual(record["status"], "fail" if code else "pass")
                self.assertEqual(record["cleanup"], "not_verified")
                self.assertEqual(record["upstream_inference"], "not_verified")

    def test_stop_failure_still_uses_real_owned_close_and_no_raw_output(self):
        adapter = FakeAdapter("stop")
        with (contextlib.redirect_stdout(io.StringIO()) as out,
              self.assertRaisesRegex(stack.gateway.Failure, "cleanup failed")):
            stack.exercise(adapter)
        self.assertIn("close", adapter.calls)
        self.assertEqual(out.getvalue(), "")

    def test_guest_operation_failures_do_not_expose_child_diagnostics(self):
        with (patch.object(stack.subprocess, "run", return_value=subprocess.CompletedProcess(
                [], 1, "secret-token", "provider-key")),
              self.assertRaisesRegex(stack.gateway.Failure, "guest operation failed") as caught):
            stack.command("unused")
        self.assertNotIn("secret", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
