"""Direct guest adapter for the fixed native-instance fixture.

This is a trusted, immutable fixture inside the scenario VM, not a
sandbox or a command transport for untrusted callers.
"""

import json
import os
import re
import subprocess
import time


class GuestFixture:
    def __init__(self):
        self.step = 0

    def _run(self, command, timeout):
        # Commands originate exclusively from the store-pinned fixture.
        return subprocess.run(
            ["/run/current-system/sw/bin/bash", "-c", command],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            stdin=subprocess.DEVNULL,
            timeout=timeout,
            check=False,
            text=True,
        )

    def _report_test_failure(self, result):
        # Only the fixed synthetic Go scenario emits these diagnostics.
        # Send bounded, escaped assertion lines to the Incus serial console
        # (not the agent status channel); never export a full guest log.
        lines = []
        for line in (result.stdout + "\n" + result.stderr).splitlines():
            if re.search(r"instance_vm_test\.go:\d+:", line):
                lines.append(re.sub(r"[0-9a-fA-F]{24,}", "<redacted>", line.strip())[:768])
                if len(lines) == 3:
                    break
        payload = json.dumps({"step": self.step, "exit_code": result.returncode, "assertions": lines})
        if len(payload) > 3500:
            payload = json.dumps({"step": self.step, "exit_code": result.returncode, "assertions": []})
        try:
            fd = os.open("/dev/ttyS0", os.O_WRONLY | os.O_NOCTTY | os.O_NONBLOCK)
            try:
                os.write(fd, ("\nAGINCTUS_FIXTURE_TEST_FAILURE:" + payload + "\n").encode("ascii"))
            finally:
                os.close(fd)
        except OSError:
            # Diagnostics must not alter the original failed assertion.
            pass

    def succeed(self, command, timeout=900):
        self.step += 1
        result = self._run(command, timeout)
        if result.returncode != 0:
            if command.endswith(" -test.run '^TestNativeInstanceVMAcceptance(f"fixture command failed (exit {result.returncode})")
        return result.stdout

    def fail(self, command, timeout=900):
        self.step += 1
        result = self._run(command, timeout)
        # A signal is an infrastructure failure, not an expected denial.
        if result.returncode <= 0 or result.returncode >= 128:
            raise RuntimeError("expected ordinary nonzero fixture command exit")
        return result.stdout

    def wait_until_succeeds(self, command, timeout=90):
        self.step += 1
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            remaining = deadline - time.monotonic()
            result = self._run(command, min(remaining, 15))
            if result.returncode == 0:
                return result.stdout
            time.sleep(min(0.25, max(0, deadline - time.monotonic())))
        raise RuntimeError("fixture readiness deadline exceeded")

    def wait_for_unit(self, unit):
        # Unit names are fixed by the shared fixture, not user input.
        if unit not in ("incus.service", "user@1000.service"):
            raise RuntimeError("unexpected fixture unit")
        self.wait_until_succeeds(f"systemctl is-active --quiet {unit}", timeout=90)


machine = GuestFixture()
"):
                self._report_test_failure(result)
            raise RuntimeError(f"fixture command failed (exit {result.returncode})")
        return result.stdout

    def fail(self, command, timeout=900):
        self.step += 1
        result = self._run(command, timeout)
        # A signal is an infrastructure failure, not an expected denial.
        if result.returncode <= 0 or result.returncode >= 128:
            raise RuntimeError("expected ordinary nonzero fixture command exit")
        return result.stdout

    def wait_until_succeeds(self, command, timeout=90):
        self.step += 1
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            remaining = deadline - time.monotonic()
            result = self._run(command, min(remaining, 15))
            if result.returncode == 0:
                return result.stdout
            time.sleep(min(0.25, max(0, deadline - time.monotonic())))
        raise RuntimeError("fixture readiness deadline exceeded")

    def wait_for_unit(self, unit):
        # Unit names are fixed by the shared fixture, not user input.
        if unit not in ("incus.service", "user@1000.service"):
            raise RuntimeError("unexpected fixture unit")
        self.wait_until_succeeds(f"systemctl is-active --quiet {unit}", timeout=90)


machine = GuestFixture()
