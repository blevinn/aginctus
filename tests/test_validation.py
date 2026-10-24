"""Exercise the real just dispatch with inert children and injected failures."""

import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class ValidationDispatchTests(unittest.TestCase):
    def test_dispatch_and_child_failures(self):
        expected = [
            "go mod verify",
            "go test ./...",
            "go vet ./...",
            "golangci-lint run",
            "ruff check .",
            "git ls-files -z -- *.sh",
            "shellcheck syntax.sh",
            *[
                f"python3 -m unittest discover -s {directory} -p test_*.py -v"
                for directory in (
                    "scripts/vm_harness",
                    "guest/opencode", "guest/ssh", "scripts/acceptance",
                    "scripts/gateway", "tests",
                )
            ],
            "shell opencode-test.sh",
            "shell opencode-live-adapter-test.sh",
            "git ls-files -z -- *.sh",
            "python3 scripts/check_markdown_links.py",
            "go run ./cmd/aginctus help",
            "python3 scripts/check_docs_examples.py",
            "nix --quiet flake check --no-build --no-update-lock-file",
            "nix --quiet flake check --no-update-lock-file",
        ]
        # Each suite stops at its own first failed command, but the reporter
        # proceeds to later suites and summarizes every observed suite.
        groups = (
            expected[:3], expected[3:7], expected[7:13], expected[13:15],
            expected[15:16], expected[16:17], expected[17:19], expected[19:],
        )
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            shutil.copy(ROOT / "justfile", root)
            shutil.copytree(ROOT / ".just", root / ".just")
            (root / "scripts/checks").mkdir(parents=True)
            shutil.copy(ROOT / "scripts/checks/report.py", root / "scripts/checks/report.py")
            tools = root / "bin"
            tools.mkdir()
            stub = f"#!{sys.executable}\n" + '''import os
from pathlib import Path
import sys
name = Path(sys.argv[0]).name
# Execute the actual reporter in the copied tree; fake only its children.
# This happens before safety assertions because the reporter must sanitize
# the intentionally hostile inherited environment.
if name == "python3" and sys.argv[1:2] == ["scripts/checks/report.py"]:
    os.execv(sys.executable, [sys.executable, *sys.argv[1:]])
command = " ".join([name, *sys.argv[1:]])
assert os.environ["AGINCTUS_INCUS_INTEGRATION"] == "0"
assert os.environ["AGINCTUS_INCUS_DISPOSABLE"] == "NO"
assert not os.environ["AGINCTUS_ACCEPTANCE_LIVE"]
assert not os.environ["AGINCTUS_ACCEPTANCE_LIVE_HOOKS"]
assert not os.environ["ACCEPTANCE_TEST_FAIL"]
assert not os.environ["GOFLAGS"]
assert os.environ["GOPROXY"] == "off"
assert os.environ["GOSUMDB"] == "off"
assert os.environ["GOTOOLCHAIN"] == "local"
with open(os.environ["DISPATCH_LOG"], "a") as log:
    log.write(command + "\\n")
if command == os.environ.get("FAIL_COMMAND"):
    sys.exit(23)
if name == "git":
    sys.stdout.buffer.write(b"syntax.sh\\0")
'''
            for name in ("go", "golangci-lint", "python3", "ruff", "git",
                         "nix", "shellcheck"):
                tool = tools / name
                tool.write_text(stub)
                tool.chmod(0o700)
            acceptance = root / "scripts/acceptance"
            acceptance.mkdir(parents=True)
            for name in ("opencode-test.sh", "opencode-live-adapter-test.sh"):
                (acceptance / name).write_text(
                    'set -eu\n'
                    '[[ "$AGINCTUS_INCUS_INTEGRATION" == 0 ]]\n'
                    '[[ "$AGINCTUS_INCUS_DISPOSABLE" == NO ]]\n'
                    '[[ -z "$AGINCTUS_ACCEPTANCE_LIVE" ]]\n'
                    '[[ -z "$AGINCTUS_ACCEPTANCE_LIVE_HOOKS" ]]\n'
                    '[[ -z "$ACCEPTANCE_TEST_FAIL" ]]\n'
                    '[[ -z "$GOFLAGS" ]]\n'
                    '[[ "$GOPROXY" == off ]]\n'
                    '[[ "$GOSUMDB" == off ]]\n'
                    '[[ "$GOTOOLCHAIN" == local ]]\n'
                    f'command="shell {name}"\n'
                    'printf "%s\\n" "$command" >> "$DISPATCH_LOG"\n'
                    '[[ "$command" != "${FAIL_COMMAND:-}" ]]\n'
                )
            syntax = root / "syntax.sh"
            log = root / "dispatch.log"
            env = dict(
                os.environ,
                PATH=f"{tools}:{os.environ['PATH']}",
                DISPATCH_LOG=str(log),
                AGINCTUS_INCUS_INTEGRATION="1",
                AGINCTUS_INCUS_DISPOSABLE="YES",
                AGINCTUS_ACCEPTANCE_LIVE="I_UNDERSTAND",
                AGINCTUS_ACCEPTANCE_LIVE_HOOKS="/must/not/execute",
                ACCEPTANCE_TEST_FAIL="1",
                GOFLAGS="-run=^$ -count=0",
                GOPROXY="https://must.not.contact.invalid",
                GOSUMDB="must.not.contact.invalid",
                GOTOOLCHAIN="must-not-select",
            )
            for failure in ("", *expected, "syntax"):
                with self.subTest(failure=failure or "success"):
                    log.write_text("")
                    syntax.write_text("if\n" if failure == "syntax" else "true\n")
                    result = subprocess.run(
                        ["just", "check-all"], cwd=root,
                        env=dict(env, FAIL_COMMAND=failure),
                        capture_output=True, text=True, timeout=30, check=False,
                    )
                    seen = log.read_text().splitlines()
                    # The docs checker is a pipe: both commands must run,
                    # but the OS can schedule either child to log first.
                    producer = "go run ./cmd/aginctus help"
                    consumer = "python3 scripts/check_docs_examples.py"
                    if producer in seen or consumer in seen:
                        first = min(
                            index for index, command in enumerate(seen)
                            if command in (producer, consumer)
                        )
                        self.assertCountEqual(
                            seen[first:first + 2], [producer, consumer],
                            "both sides of the documentation-check pipe must execute",
                        )
                        seen[first:first + 2] = [producer, consumer]
                    if not failure:
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertIn("Hermetic: 8 passed, 0 failed, 0 errors", result.stdout)
                    else:
                        self.assertNotEqual(result.returncode, 0, result.stderr)
                        failed_groups = sum(failure in commands for commands in groups)
                        if failure == "syntax":
                            failed_groups = 1
                        self.assertIn(
                            f"Hermetic: {8 - failed_groups} passed, "
                            f"{failed_groups} failed, 0 errors",
                            result.stdout,
                        )
                    observed = []
                    for commands in groups:
                        if failure in commands:
                            # Failed Just dependencies and multiline recipes
                            # stop within their suite, not the entire run.
                            length = commands.index(failure) + 1
                            if failure in (producer, consumer):
                                # The pipe launches both sides.
                                length = len(commands)
                            observed.extend(commands[:length])
                        else:
                            observed.extend(commands)
                    self.assertEqual(seen, observed)


if __name__ == "__main__":
    unittest.main()
