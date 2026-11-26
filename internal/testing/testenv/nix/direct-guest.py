"""Direct guest adapter for the fixed native-instance fixture.

This is a trusted, immutable fixture inside the scenario VM, not a
sandbox or a command transport for untrusted callers.
"""

import subprocess
import time


class GuestFixture:
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

    def succeed(self, command, timeout=900):
        result = self._run(command, timeout)
        if result.returncode != 0:
            raise RuntimeError(f"fixture command failed (exit {result.returncode})")
        return result.stdout

    def fail(self, command, timeout=900):
        result = self._run(command, timeout)
        # A signal is an infrastructure failure, not an expected denial.
        if result.returncode <= 0 or result.returncode >= 128:
            raise RuntimeError("expected ordinary nonzero fixture command exit")
        return result.stdout

    def wait_until_succeeds(self, command, timeout=90):
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
