"""Verify bounded failure-case parsing without real Go, Nix, or Incus."""

import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/checks/report.py"
SPEC = importlib.util.spec_from_file_location("test_report", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
report = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(report)


class FailureCaseTests(unittest.TestCase):
    def test_python_fragmentation_deduplication_and_oversize(self):
        cases = report.FailureCases("python")
        line = b"FAIL: test_one (test_foo.Case.test_one)\n"
        cases.feed(line[:13])
        cases.feed(line[13:])
        cases.feed(line)
        cases.feed(b"ERROR: test_two (test_foo.Case.test_two)\n")
        cases.feed(b"FAIL: test_bad (something;BAD=SECRET)\n")
        cases.feed(b"X" * (report.MAX_LINE_BYTES * 3) + b"\n")
        cases.feed(b"FAIL: test_three (test_foo.Case.test_three)\n")
        self.assertEqual(
            cases.cases,
            [
                {"id": "test_foo.Case.test_one", "kind": "fail"},
                {"id": "test_foo.Case.test_two", "kind": "error"},
                {"id": "test_foo.Case.test_three", "kind": "fail"},
            ],
        )
        self.assertFalse(cases.truncated)

    def test_go_events_only_real_fails(self):
        cases = report.FailureCases("go")
        events = [
            {"Action": "output", "Package": "github.com/blevinn/aginctus/internal/cli",
             "Test": "TestOne", "Output": "SECRET_CANARY"},
            {"Action": "fail", "Package": "github.com/blevinn/aginctus/internal/cli"},
            {"Action": "fail", "Package": "github.com/blevinn/aginctus/internal/cli",
             "Test": "TestOne"},
            {"Action": "fail", "Package": "github.com/other/project", "Test": "TestTwo"},
            {"Action": "fail", "Package": "github.com/blevinn/aginctusbad",
             "Test": "TestThree"},
        ]
        for event in events:
            cases.feed((json.dumps(event) + "\n").encode())
        self.assertEqual(
            cases.cases,
            [{"id": "github.com/blevinn/aginctus/internal/cli:TestOne",
              "kind": "fail"}],
        )

    def test_limit_and_truncation(self):
        cases = report.FailureCases("python")
        for number in range(20):
            cases.feed(
                f"FAIL: test_{number} (test_foo.Case.test_{number})\n".encode()
            )
        self.assertEqual(len(cases.cases), report.MAX_CASES)
        self.assertTrue(cases.truncated)

    def test_subprocess_failures_do_not_leak_raw_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "just"
            binary.write_text(
                "#!/bin/sh\n"
                'test "$AGINCTUS_INCUS_INTEGRATION" = 0 || exit 42\n'
                'test -z "$AGINCTUS_ACCEPTANCE_LIVE" || exit 42\n'
                'if [ "$2" = go ]; then\n'
                '  test "$AGINCTUS_CHECK_STRUCTURED" = 1 || exit 42\n'
                "  echo '{\"Action\":\"fail\",\"Package\":\"github.com/blevinn/aginctus/internal/cli\",\"Test\":\"TestGo\"}'\n"
                '  echo SECRET_CANARY >&2\n'
                '  exit 17\n'
                'fi\n'
                'if [ "$2" = python ]; then\n'
                "  echo 'FAIL: test_python (test_foo.Case.test_python)' >&2\n"
                '  echo SECRET_CANARY\n'
                '  exit 21\n'
                'fi\n'
                'echo SECRET_CANARY\n',
                encoding="utf-8",
            )
            binary.chmod(0o700)
            env = dict(os.environ, PATH=f"{temporary}:{os.environ.get('PATH', '')}")
            env.update(report.SAFE_ENV)
            go = report.run_suite("go", env, False)
            python = report.run_suite("python", env, False)
            other = report.run_suite("lint", env, False)
            self.assertEqual((go["status"], go["exit_code"]), ("fail", 17))
            self.assertEqual((python["status"], python["exit_code"]), ("fail", 21))
            self.assertEqual(other["status"], "pass")
            self.assertEqual(
                go["failed_cases"],
                [{"id": "github.com/blevinn/aginctus/internal/cli:TestGo",
                  "kind": "fail"}],
            )
            self.assertEqual(
                python["failed_cases"],
                [{"id": "test_foo.Case.test_python", "kind": "fail"}],
            )
            self.assertNotIn("SECRET_CANARY", json.dumps([go, python, other]))


if __name__ == "__main__":
    unittest.main()
