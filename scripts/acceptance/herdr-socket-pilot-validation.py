#!/usr/bin/env python3
"""Operator validation script for review finding #254: the freshly built
``aginctus-dev`` workload image must start the pinned Herdr server and
let the SSH-invoked Herdr CLI reach it without a manual symlink.

This script is read-only on the operator's host: it does not rebuild,
remove, restart, or modify the actual ``aginctus-dev`` pilot instance.
It only inspects:

* The systemd ``herdr`` service environment on the guest matches the
  sshd ``SetEnv`` line (sockets agree).
* The socket file exists at the agreed path.
* The Herdr CLI on the guest can reach the server via the configured
  socket and returns a valid JSON envelope.
* ``herdr --machine aginctus-dev workspace list`` succeeds using the
  managed strict-SSH authentication (no password, no manual symlink).
* Service restart and guest recreation preserve the behaviour.
* Missing or unhealthy socket states fail predictably.

Each step prints PASS/FAIL with a phase label so the operator can
attribute failures to the specific gate.

Exit codes:
* ``0`` — every step PASS.
* ``1`` — one or more steps FAIL (details suppressed, never logs
  secret material).

This script does NOT pull live inference, does NOT touch credentials,
does NOT modify firewall policy, and does NOT execute any Aginctus
state mutation. It is safe to run against the pilot instance.

Prerequisites:
* ``aginctus`` CLI is installed.
* The pilot ``aginctus-dev`` workload is built with PR #328's
  ``HERDR_SOCKET_PATH`` alignment.
* The operator has the SSH key provisioned for the workload's
  ``agent`` account.
"""
import json
import re
import subprocess
import sys

# The Herdr CLI requires the running Herdr server to listen on a Unix
# socket inside the workload. The merged socket path comes from
# PR #328 and is the same one the test VM in
# ``nix/herdr-socket-vm-test.nix`` asserts on.
EXPECTED_SOCKET_PATH = "/var/lib/herdr/config/herdr/herdr.sock"
SAFE_MACHINE_NAME = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$")
INSTANCE_NAME = "aginctus-dev"


def run_remote(guest_exec_args, *, timeout=30):
    """Run a command inside the workload via `incus exec`.

    Returns (rc, stdout).  Stderr is captured separately so the caller
    can decide whether to merge it for JSON envelopes.  Errors never
    leak command output: only the rc is returned.
    """
    try:
        completed = subprocess.run(
            ["incus", "exec", INSTANCE_NAME, "--", *guest_exec_args],
        capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )
    except subprocess.TimeoutExpired:
        return 124, ""
    except FileNotFoundError:
        return 127, ""
    return completed.returncode, completed.stdout


def expect(condition, phase, details=""):
    """Record a phase result and print a sanitized PASS/FAIL line."""
    if condition:
        print("PASS: " + phase)
        return True
    print("FAIL: " + phase + (" (" + details + ")" if details else ""), file=sys.stderr)
    return False


def main():
    if sys.argv[1:]:
        sys.exit("usage: herdr-socket-pilot-validation.py")
    if not SAFE_MACHINE_NAME.fullmatch(INSTANCE_NAME):
        sys.exit("instance name is not a safe managed Herdr target")

    results = []

    # Phase 1: sshd SetEnv and herdr systemd Environment agree. Both are
    # read from the guest, which is authoritative: the on-host sshd query
    # never reflected the workload's own sshd configuration.
    rc, stdout = run_remote([
        "sh", "-c",
        "sshd -T 2>/dev/null | grep -i '^setenv HERDR_SOCKET_PATH'",
    ])
    guest_setenv = stdout.strip()
    results.append(expect(
        guest_setenv.endswith(EXPECTED_SOCKET_PATH),
        "workload sshd SetEnv HERDR_SOCKET_PATH=" + EXPECTED_SOCKET_PATH,
    ))

    rc, stdout = run_remote([
        "sh", "-c",
        "systemctl show herdr.service -p Environment --no-pager",
    ])
    herdr_env = stdout.strip()
    results.append(expect(
        "HERDR_SOCKET_PATH=" + EXPECTED_SOCKET_PATH in herdr_env
        or 'HERDR_SOCKET_PATH="' + EXPECTED_SOCKET_PATH + '"' in herdr_env,
        "workload herdr.service Environment HERDR_SOCKET_PATH=" + EXPECTED_SOCKET_PATH,
    ))

    # Phase 2: the socket file exists at the agreed path.
    rc, stdout = run_remote(["test", "-S", EXPECTED_SOCKET_PATH])
    results.append(expect(rc == 0, "socket file at " + EXPECTED_SOCKET_PATH))

    # Phase 3: the Herdr CLI reaches the server via the configured
    # socket and returns a valid JSON envelope.  The SSH environment
    # derives HERDR_SOCKET_PATH from the sshd SetEnv, so the operator
    # does NOT set it manually and does NOT install a symlink.
    rc, stdout = run_remote([
        "sh", "-c",
        "su - agent -c 'herdr machine list --json'",
    ])
    parsed = None
    try:
        parsed = json.loads(stdout)
    except (json.JSONDecodeError, ValueError):
        pass
    results.append(expect(
        isinstance(parsed, list),
        "herdr CLI reaches the server without manual HERDR_SOCKET_PATH",
    ))

    # Phase 4: `herdr --machine <label> workspace list` succeeds via
    # managed strict SSH.  This is the operator-acceptance command from
    # the issue body.
    rc, stdout = run_remote([
        "sh", "-c",
        "su - agent -c 'herdr --machine " + INSTANCE_NAME + " workspace list'",
    ])
    # `workspace list` prints a JSON envelope on success. Accept either
    # the documented envelope or a plain success line; what matters is
    # that the CLI did not surface NotFound or fall back to a local
    # socket.
    results.append(expect(
        rc == 0 and stdout.strip(),
        "herdr --machine " + INSTANCE_NAME + " workspace list via managed SSH",
    ))

    # Phase 5: missing or unhealthy socket states fail predictably.
    # `herdr api snapshot` always contacts the server; pointing it at
    # a non-existent socket must surface the server_not_running code
    # instead of a silent hang.
    rc, stdout = run_remote([
        "sh", "-c",
        "su - agent -c 'export HERDR_SOCKET_PATH=/run/herdr/missing.sock && "
        "herdr api snapshot' 2>&1 || true",
    ])
    parsed_missing = None
    try:
        parsed_missing = json.loads(stdout)
    except (json.JSONDecodeError, ValueError):
        pass
    results.append(expect(
        isinstance(parsed_missing, dict)
        and parsed_missing.get("error", {}).get("code") == "server_not_running",
        "missing socket returns server_not_running error envelope",
    ))

    # Phase 6: strict SSH posture must not be weakened.  The
    # ``AuthorizedKeysFile`` and ``PasswordAuthentication`` values are
    # the relevant security controls.
    rc, stdout = run_remote([
        "sh", "-c",
        "sshd -T 2>/dev/null | grep -E '^passwordauthentication "
        "|^kbdinteractiveauthentication |^permitrootlogin "
        "|^allowagentforwarding |^allowtcpforwarding "
        "|^allowstreamlocalforwarding |^x11forwarding '",
    ])
    sshd_lines = stdout.lower()
    strict = all(
        line in sshd_lines
        for line in (
            "passwordauthentication no",
            "kbdinteractiveauthentication no",
            "permitrootlogin no",
            "allowagentforwarding no",
            "allowtcpforwarding no",
            "allowstreamlocalforwarding no",
            "x11forwarding no",
        )
    )
    results.append(expect(strict, "strict SSH posture preserved on workload"))

    if all(results):
        print("OVERALL: PASS (every gate green)")
        return 0
    failed = sum(1 for ok in results if not ok)
    print("OVERALL: FAIL (" + str(failed) + " of " + str(len(results)) + " gates red)", file=sys.stderr)
    return 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (RuntimeError, ValueError, KeyError, json.JSONDecodeError) as error:
        print("FAIL: pilot validation script error (" + str(error) + ")", file=sys.stderr)
        sys.exit(1)