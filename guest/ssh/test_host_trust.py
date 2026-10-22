"""Hermetic regression tests for guest/ssh/host-trust.sh.

The script writes to the hardcoded ``/var/lib/aginctus/ssh`` path and is
expected to run as root in a guest image. Each test runs the script inside
an ``unshare --user --map-root-user --mount`` namespace and bind-mounts a
process-local temp directory onto ``/var/lib``.

Negative-path fixtures that need a non-root-owned file or directory rely on
``unshare --map-users=<inner>:<host-uid>:1``. On hosts where the running
user has no ``/etc/subuid`` entries, the namespace UID mapping is limited
to root only, so the script cannot see non-root ownership for arbitrary
files and the negative tests are skipped with a clear reason.
"""
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "guest" / "ssh" / "host-trust.sh"


def namespace_can_chown_to_foreign():
    """Return True iff we can chown to a non-root uid inside the namespace."""
    # ``unshare --user --map-root-user --map-users=N:H:1`` may be accepted
    # by the kernel even without ``/etc/subuid`` entries, but a subsequent
    # ``chown N:N`` still fails because the gid is unmapped. Probe with
    # an actual chown to detect both layers of the requirement.
    fd, path = tempfile.mkstemp(prefix="aginctus-host-trust-probe-")
    os.close(fd)
    try:
        probe = subprocess.run(
            ["unshare", "--user", "--map-root-user",
             "--map-users=1234:1000:1", "bash", "-c",
             f"chown 1234 {path}"],
            capture_output=True, text=True, timeout=10, check=False,
        )
        return probe.returncode == 0
    finally:
        os.unlink(path)


class _NamespaceTestCase(unittest.TestCase):
    """Base class that runs commands inside an unshare namespace."""

    @classmethod
    def setUpClass(cls):
        cls.can_chown_foreign = namespace_can_chown_to_foreign()

    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="aginctus-host-trust-"))
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)

    def _run_unshare(self, script_args, *, setup="", mount_target="/var/lib",
                     extra_unshare=(), stdin_text=None):
        cmd = [
            "unshare", "--user", "--map-root-user",
            *extra_unshare,
            "--mount", "--fork",
            "--", "bash", "-c",
            f"set -e; mount --bind {self.tmp} {mount_target}; {setup}; "
            f"exec bash {SCRIPT} {script_args}",
        ]
        return subprocess.run(
            cmd, input=stdin_text,
            capture_output=True, text=True, timeout=30, check=False,
        )

    def fresh_layout(self, perms_root="700", perms_trust="700"):
        return (
            "rm -rf /var/lib/aginctus; "
            "mkdir -p /var/lib/aginctus/ssh/trust; "
            f"chmod {perms_root} /var/lib/aginctus/ssh; "
            f"chmod {perms_trust} /var/lib/aginctus/ssh/trust"
        )


class HostTrustRootOwnershipTests(_NamespaceTestCase):
    """A15 regression coverage for the host-trust.sh ownership guards."""

    def test_rejects_unsafe_trust_directory_permissions(self):
        setup = self.fresh_layout(perms_trust="755")
        result = self._run_unshare("inspect aginctus-test", setup=setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsafe trust directory permissions", result.stderr)

    def test_rejects_unsafe_known_hosts_permissions(self):
        setup = (
            self.fresh_layout() +
            "; touch /var/lib/aginctus/ssh/trust/known_hosts; "
            "chmod 644 /var/lib/aginctus/ssh/trust/known_hosts"
        )
        result = self._run_unshare("inspect aginctus-test", setup=setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsafe known_hosts permissions", result.stderr)

    def test_rejects_invalid_state_root_type(self):
        setup = (
            "rm -rf /var/lib/aginctus; "
            "mkdir -p /var/lib/aginctus; "
            "touch /var/lib/aginctus/ssh"  # file instead of directory
        )
        result = self._run_unshare("inspect aginctus-test", setup=setup)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid ssh state root", result.stderr)

    def test_rejects_non_root_owned_trust_directory(self):
        if not self.can_chown_foreign:
            self.skipTest("namespace UID mapping unavailable on this host")
        setup = (
            "rm -rf /var/lib/aginctus; "
            "mkdir -p /var/lib/aginctus/ssh/trust; "
            "chmod 700 /var/lib/aginctus/ssh /var/lib/aginctus/ssh/trust; "
            "chown 1234:1234 /var/lib/aginctus/ssh/trust"
        )
        result = self._run_unshare(
            "inspect aginctus-test", setup=setup,
            extra_unshare=("--map-users=1234:1000:1",),
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("trust directory not root-owned", result.stderr)

    def test_rejects_non_root_owned_state_root(self):
        if not self.can_chown_foreign:
            self.skipTest("namespace UID mapping unavailable on this host")
        setup = (
            "rm -rf /var/lib/aginctus; "
            "mkdir -p /var/lib/aginctus/ssh/trust; "
            "chmod 700 /var/lib/aginctus/ssh /var/lib/aginctus/ssh/trust; "
            "chown 1234:1234 /var/lib/aginctus/ssh"
        )
        result = self._run_unshare(
            "inspect aginctus-test", setup=setup,
            extra_unshare=("--map-users=1234:1000:1",),
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ssh state root not root-owned", result.stderr)

    def test_rejects_non_root_owned_known_hosts(self):
        if not self.can_chown_foreign:
            self.skipTest("namespace UID mapping unavailable on this host")
        setup = (
            self.fresh_layout() +
            "; touch /var/lib/aginctus/ssh/trust/known_hosts; "
            "chmod 600 /var/lib/aginctus/ssh/trust/known_hosts; "
            "chown 1234:1234 /var/lib/aginctus/ssh/trust/known_hosts"
        )
        result = self._run_unshare(
            "inspect aginctus-test", setup=setup,
            extra_unshare=("--map-users=1234:1000:1",),
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("known_hosts not root-owned", result.stderr)

    def test_round_trip_with_root_owned_layout(self):
        # Positive sanity check: a properly owned, mode-700 layout accepts
        # the reconcile operation and surfaces the pinned entry on inspect.
        keydir = tempfile.mkdtemp(prefix="aginctus-host-trust-key-")
        self.addCleanup(shutil.rmtree, keydir, ignore_errors=True)
        subprocess.run(
            ["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "test",
             "-f", os.path.join(keydir, "key")],
            check=True, capture_output=True,
        )
        pub = (Path(keydir) / "key.pub").read_text()
        setup = self.fresh_layout()
        reconcile = self._run_unshare(
            "reconcile aginctus-test", setup=setup, stdin_text=pub,
        )
        self.assertEqual(reconcile.returncode, 0,
                         msg=f"stderr: {reconcile.stderr}")
        self.assertIn("aginctus-test", reconcile.stdout)

        # Second invocation reuses the same temp dir; the reconcile already
        # left a known_hosts file, so the inspect path only needs to ensure
        # the existing ownership/mode remains root-owned/700.
        inspect_setup = (
            "chmod 700 /var/lib/aginctus/ssh; "
            "chmod 700 /var/lib/aginctus/ssh/trust"
        )
        inspect = self._run_unshare(
            "inspect aginctus-test", setup=inspect_setup,
        )
        self.assertEqual(inspect.returncode, 0,
                         msg=f"stderr: {inspect.stderr}")
        self.assertIn("aginctus-test", inspect.stdout)


if __name__ == "__main__":
    unittest.main()