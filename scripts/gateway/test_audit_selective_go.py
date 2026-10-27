"""Host-only audit boundaries; no Incus daemon or pilot is contacted."""

import argparse
import copy
import hashlib
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import MagicMock, Mock, patch

import audit_selective_go as audit


class AuditTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.bundle = Path(self.temp.name) / "bundle"
        self.bundle.mkdir()
        self.hashes = {}
        for name, source in audit.SOURCES.items():
            data = (Path(audit.__file__).parent / source).read_bytes()
            (self.bundle / name).write_bytes(data)
            self.hashes[name] = hashlib.sha256(data).hexdigest()
        (self.bundle / "manifest.json").write_text(json.dumps(self.hashes))
        self.args = argparse.Namespace(
            project="explicit-gateway", instance="litellm-1", bundle_dir=str(self.bundle),
            expected_image="ghcr.io/berriai/litellm:v1.103.0@sha256:" + "a" * 64,
            expected_incus_fingerprint="b" * 64,
        )
        self.instance = {
            "name": self.args.instance, "status": "Running",
            "config": {
                "user.aginctus.managed": "true",
                "user.aginctus.resource": "compose-instance",
                "user.aginctus.deployment": self.args.project,
                "user.label.incus-compose.service": "litellm",
                "user.image_alias": "ic-image-vviggpnjyqkklvoyynm7ojd5225xhng3h5n3g4654em6nkzngmsa",
                "volatile.base_image": "b" * 64,
            },
            "expanded_config": {"environment." + k: v for k, v in audit.ENVIRONMENT.items()},
        }
        self.success = {"result": "PASS", "hashes": self.hashes}

    def assert_failure(self, label, function, *args):
        with self.assertRaises(audit.AuditFailure) as caught:
            function(*args)
        self.assertEqual(caught.exception.label, label)

    def test_success_explicit_query_then_fixed_exec(self):
        with patch.object(audit, "command_json", side_effect=[self.instance, self.success]) as run:
            self.assertEqual(audit.audit(self.args), "PASS")
        self.assertEqual(run.call_args_list[0].args[0], [
            "incus", "query", "/1.0/instances/litellm-1?project=explicit-gateway",
        ])
        self.assertEqual(run.call_args_list[1].args[0], [
            "incus", "exec", "--project", "explicit-gateway", "litellm-1", "--",
            "/app/.venv/bin/python", "-I", "-B", "-c", audit.GUEST_SCRIPT,
        ])

    def test_no_exec_before_all_host_checks(self):
        cases = [("name", "other", "FAIL_OWNERSHIP"),
                 ("status", "Stopped", "FAIL_OWNERSHIP")]
        for key in self.instance["config"]:
            cases.append((key, "untrusted-secret", "FAIL_IMAGE" if key in (
                "user.image_alias", "volatile.base_image") else "FAIL_OWNERSHIP"))
        for key, value, label in cases:
            with self.subTest(key=key):
                instance = copy.deepcopy(self.instance)
                target = instance if key in ("name", "status") else instance["config"]
                target[key] = value
                with patch.object(audit, "command_json", return_value=instance) as run:
                    self.assert_failure(label, audit.audit, self.args)
                    self.assertEqual(run.call_count, 1)
        for key in audit.ENVIRONMENT:
            instance = copy.deepcopy(self.instance)
            instance["expanded_config"]["environment." + key] = "/untrusted"
            with patch.object(audit, "command_json", return_value=instance) as run:
                self.assert_failure("FAIL_ENV", audit.audit, self.args)
                self.assertEqual(run.call_count, 1)

    def test_image_inputs_and_only_upstream_normalization(self):
        for image in ("ghcr.io/berriai/litellm:latest", "other@sha256:" + "a" * 64,
                      "ghcr.io/berriai/litellm@sha256:" + "A" * 64):
            self.args.expected_image = image
            with patch.object(audit, "command_json") as run:
                self.assert_failure("FAIL_INPUT", audit.audit, self.args)
                run.assert_not_called()
        self.args.expected_image = "ghcr.io/berriai/litellm@sha256:" + "a" * 64
        with patch.object(audit, "command_json", side_effect=[self.instance, self.success]):
            self.assertEqual(audit.audit(self.args), "PASS")
        self.instance["config"]["user.image_alias"] += "extra"
        with patch.object(audit, "command_json", return_value=self.instance):
            self.assert_failure("FAIL_IMAGE", audit.audit, self.args)

    def test_bounded_alias_matches_importer_golden_and_rejects_other_spellings(self):
        image = "ghcr.io/berriai/litellm:v1.103.0@sha256:bd089afdcd35b894b14a93f9743cdc8b591f82da1a38dd43a010a7b0c9de5fd7"
        self.assertEqual(audit.incus_image_alias(image),
                         "ic-image-eb2dh5p4ong4xc43eddosshm2xl4nntvmxxub2ksgsmeqtjkyzwa")
        self.assertLessEqual(len(audit.incus_image_alias(image)), 64)
        for wrong in ("ghcr.io/berriai/litellm@sha256:" + "a" * 64,
                      "ghcr.io/berriai/litellm:v1.103.0", "ic-image-" + "a" * 52):
            self.instance["config"]["user.image_alias"] = wrong
            with patch.object(audit, "command_json", return_value=self.instance) as run:
                self.assert_failure("FAIL_IMAGE", audit.audit, self.args)
                self.assertEqual(run.call_count, 1)

    def test_bundle_drift_symlinks_manifest_and_extra_entries(self):
        self.assertEqual(audit.bundle_hashes(self.bundle), self.hashes)
        config = self.bundle / "config.yaml"
        original = config.read_bytes()
        for data in (original + b"model_list: []\n", original.replace(b"callbacks", b"callback")):
            config.write_bytes(data)
            self.assert_failure("FAIL_BUNDLE", audit.bundle_hashes, self.bundle)
        config.unlink()
        config.symlink_to(Path(audit.__file__).parent / "selective-go.yaml")
        self.assert_failure("FAIL_BUNDLE", audit.bundle_hashes, self.bundle)
        config.unlink()
        config.write_bytes(original)
        manifest = self.bundle / "manifest.json"
        for data in ("{}", "not JSON", '{"config.yaml":"x","config.yaml":"y"}',
                     json.dumps({**self.hashes, "config.yaml": "0" * 64})):
            manifest.write_text(data)
            self.assert_failure("FAIL_BUNDLE", audit.bundle_hashes, self.bundle)
        manifest.write_text(json.dumps(self.hashes))
        (self.bundle / "unexpected").touch()
        self.assert_failure("FAIL_BUNDLE", audit.bundle_hashes, self.bundle)

    def test_guest_output_allowlist_and_hash_drift(self):
        for value in ([], {}, {"result": "secret"}, {"result": ["PASS"]},
                      {"result": "FAIL_HEALTH", "stderr": "secret"},
                      {"result": "PASS", "hashes": {}}):
            with patch.object(audit, "command_json", side_effect=[self.instance, value]):
                label = "FAIL_FILES" if isinstance(value, dict) and value.get("hashes") == {} else "FAIL_GUEST"
                self.assert_failure(label, audit.audit, self.args)

    def test_main_suppresses_input_and_errors(self):
        for argv in ([], ["--project", "raw-secret"], ["--unknown-secret"]):
            output = io.StringIO()
            with redirect_stdout(output):
                self.assertEqual(audit.main(argv), 1)
            self.assertEqual(json.loads(output.getvalue()), {"result": "FAIL_INPUT"})
        argv = []
        for key, value in vars(self.args).items():
            argv.extend(["--" + key.replace("_", "-"), value])
        for error in (audit.AuditFailure("FAIL_QUERY"), RuntimeError("secret-stderr")):
            output = io.StringIO()
            with patch.object(audit, "audit", side_effect=error), redirect_stdout(output):
                self.assertEqual(audit.main(argv), 1)
            self.assertNotIn("secret", output.getvalue())
            self.assertIn(json.loads(output.getvalue())["result"], audit.LABELS)

    def test_mocked_subprocess_never_executes_foreign_guest(self):
        self.instance["config"]["user.aginctus.managed"] = "false"
        real_popen = subprocess.Popen

        def query_only(command, **kwargs):
            self.assertEqual(command[:2], ["incus", "query"])
            return real_popen([sys.executable, "-I", "-c",
                               "print(" + repr(json.dumps(self.instance)) + ")"], **kwargs)

        with patch.object(audit.subprocess, "Popen", side_effect=query_only) as run:
            self.assert_failure("FAIL_OWNERSHIP", audit.audit, self.args)
            self.assertEqual(run.call_count, 1)

    def test_subprocess_output_and_timeout_bounds(self):
        # Disposable local Python processes exercise the actual pipe reader.
        for script in ("print('secret stderr', file=__import__('sys').stderr); raise SystemExit(1)",
                       "print('s' * 8192)", "print('not-json-secret')"):
            self.assert_failure("FAIL_QUERY", audit.command_json,
                                [sys.executable, "-I", "-c", script], 4096, "FAIL_QUERY")
        with patch.object(audit.subprocess, "Popen", side_effect=subprocess.TimeoutExpired("secret", 1)):
            self.assert_failure("FAIL_GUEST", audit.command_json, ["unused"], 4096, "FAIL_GUEST")
        with patch.object(audit.subprocess, "Popen", side_effect=OSError("secret")) as run:
            self.assert_failure("FAIL_QUERY", audit.command_json, ["unused"], 4096, "FAIL_QUERY")
        self.assertEqual(run.call_args.kwargs["stdin"], subprocess.DEVNULL)
        self.assertEqual(run.call_args.kwargs["stderr"], subprocess.DEVNULL)
        self.assertFalse(run.call_args.kwargs["shell"])
        process = MagicMock()
        process.__enter__.return_value = process
        process.poll.return_value = None
        selector = MagicMock()
        selector.__enter__.return_value = selector
        selector.select.return_value = []
        with patch.object(audit.subprocess, "Popen", return_value=process), \
                patch.object(audit.selectors, "DefaultSelector", return_value=selector):
            self.assert_failure("FAIL_GUEST", audit.command_json, ["unused"], 4096, "FAIL_GUEST")
        process.kill.assert_called_once()
        process.wait.assert_called_once()
        self.assertLessEqual(selector.select.call_args.args[0], 20)

    def guest(self, **changes):
        """Execute the unchanged fixed script with a mapped root and mocked I/O."""
        root = Path(self.temp.name) / "guest"
        delivery = root / "app/aginctus-go"
        delivery.mkdir(parents=True, exist_ok=True)
        for name in audit.SOURCES:
            target = delivery / name
            if target.exists() or target.is_symlink():
                target.unlink()
            target.write_bytes((self.bundle / name).read_bytes())
        if changes.get("symlink"):
            (delivery / "config.yaml").unlink()
            (delivery / "config.yaml").symlink_to(self.bundle / "config.yaml")
        if changes.get("writable"):
            (delivery / "config.yaml").chmod(0o666)
        opener = MagicMock()
        response = opener.open.return_value.__enter__.return_value = Mock(status=200)
        if changes.get("health"):
            opener.open.side_effect = OSError("secret-health-error")
        real_open, real_stat = os.open, os.fstat

        def mapped_open(path, *args, **kwargs):
            return real_open(str(root) if path == "/" else path, *args, **kwargs)

        def owned_stat(fd):
            fields = list(real_stat(fd))
            fields[4] = 123 if changes.get("owner") else 0
            return os.stat_result(fields)

        output = io.StringIO()
        env = {**audit.ENVIRONMENT, **changes.get("env", {})}
        with patch.dict(os.environ, env, clear=True), patch("os.open", side_effect=mapped_open), \
                patch("os.fstat", side_effect=owned_stat), \
                patch("importlib.metadata.version", return_value=changes.get("version", "1.103.0")), \
                patch("urllib.request.build_opener", return_value=opener), redirect_stdout(output):
            exec(compile(audit.GUEST_SCRIPT, "<fixed-guest-audit>", "exec"), {})
        return json.loads(output.getvalue()), opener, response

    def test_guest_files_env_version_and_health(self):
        result, opener, _ = self.guest()
        self.assertEqual(result, self.success)
        self.assertEqual([call.args[0] for call in opener.open.call_args_list], [
            "http://127.0.0.1:4000/health/liveliness", "http://127.0.0.1:4000/health/readiness",
        ])
        for call in opener.open.call_args_list:
            self.assertEqual(call.kwargs, {"timeout": 3})
        for changes, label in (({"symlink": True}, "FAIL_FILES"),
                               ({"writable": True}, "FAIL_FILES"),
                               ({"owner": True}, "FAIL_FILES"),
                               ({"env": {"PYTHONPATH": "/wrong"}}, "FAIL_ENV"),
                               ({"version": "other"}, "FAIL_VERSION"),
                               ({"health": True}, "FAIL_HEALTH")):
            with self.subTest(changes=changes):
                result, _, _ = self.guest(**changes)
                self.assertEqual(result, {"result": label})


if __name__ == "__main__":
    unittest.main()
