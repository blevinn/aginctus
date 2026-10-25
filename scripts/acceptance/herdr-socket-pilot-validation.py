#!/usr/bin/env python3
"""Read-only existing-pilot checks for #254, without inference or bootstrap.

Remote machine operations run in the Herdr client, not the workload. Missing
credentials are reported by exit code, never created. This does not establish
fresh-guest, restart or recreation acceptance; those need separate evidence.
"""
import json
import shlex
import subprocess
import sys

EXPECTED_SOCKET_PATH = "/var/lib/herdr/config/herdr/herdr.sock"
EXPECTED_CONFIG_HOME = "/var/lib/herdr/config"
CLIENT = "aginctus-herdr"
WORKLOAD = "aginctus-dev"


def run_remote(instance, arguments, *, merge_stderr=False):
    """Capture helper protocols internally; never print guest output."""
    try:
        result = subprocess.run(
            ["incus", "--project", "default", "exec", instance, "--", *arguments],
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT if merge_stderr else subprocess.PIPE,
            text=True, timeout=30, check=False,
        )
    except subprocess.TimeoutExpired:
        return 124, ""
    except OSError:
        return 127, ""
    return result.returncode, result.stdout


def expect(condition, phase, details=""):
    print(("PASS: " if condition else "FAIL: ") + phase
          + (" (" + details + ")" if details else ""),
          file=sys.stdout if condition else sys.stderr)
    return bool(condition)


def parse_json(output):
    try:
        return json.loads(output)
    except ValueError:
        return None


def inspect(instance, helper, *arguments):
    rc, output = run_remote(instance, [helper, "inspect", *arguments])
    classification = {0: "present", 3: "absent", 1: "unsafe/inconsistent",
                      2: "invalid invocation"}.get(rc, "transport/helper failure")
    ok = expect(rc == 0, instance + " " + helper, f"rc={rc}, {classification}")
    return ok, output


def main():
    if sys.argv[1:]:
        print("usage: herdr-socket-pilot-validation.py", file=sys.stderr)
        return 2
    results = []

    rc, output = run_remote(WORKLOAD, ["sshd", "-T"])
    sshd = {key.lower(): value for line in output.splitlines() if " " in line
            for key, value in [line.split(" ", 1)]}
    sshd_setenv = shlex.split(sshd.get("setenv", ""))
    results.append(expect(rc == 0 and
                          "HERDR_SOCKET_PATH=" + EXPECTED_SOCKET_PATH in sshd_setenv
                          and "XDG_CONFIG_HOME=" + EXPECTED_CONFIG_HOME in sshd_setenv,
                          "workload sshd explicit-session socket environment"))
    results.append(expect(rc == 0 and all(
        sshd.get(key) == value for key, value in {
            "passwordauthentication": "no", "kbdinteractiveauthentication": "no",
            "permitrootlogin": "no", "allowagentforwarding": "no",
            "allowtcpforwarding": "no", "allowstreamlocalforwarding": "no",
            "x11forwarding": "no", "permittunnel": "no", "permituserrc": "no",
        }.items()), "strict workload SSH posture"))

    rc, output = run_remote(WORKLOAD, [
        "systemctl", "show", "herdr.service", "-p", "Environment", "--value"])
    results.append(expect(rc == 0 and
                          "HERDR_SOCKET_PATH=" + EXPECTED_SOCKET_PATH in shlex.split(output),
                          "workload systemd socket environment"))
    rc, _ = run_remote(WORKLOAD, ["test", "-S", EXPECTED_SOCKET_PATH])
    results.append(expect(rc == 0, "workload socket exists"))
    rc, _ = run_remote(WORKLOAD, ["test", "!", "-L", "/home/agent/.config/herdr"])
    results.append(expect(rc == 0, "manual socket symlink absent"))

    identity_ok, identity = inspect(CLIENT, "aginctus-ssh-client-identity")
    trust_ok, _ = inspect(CLIENT, "aginctus-ssh-host-trust", WORKLOAD)
    authorization_ok, authorization = inspect(WORKLOAD, "aginctus-ssh-authorize", "agent")
    results.extend([identity_ok, trust_ok, authorization_ok])
    if identity_ok and authorization_ok:
        keys = [line.removeprefix("public_key\t") for line in identity.splitlines()
                if line.startswith("public_key\t")]
        results.append(expect(len(keys) == 1 and keys[0] == authorization.strip(),
                              "workload authorization matches client identity"))
    if not all(results):
        print("BLOCKED: remote probes require healthy socket, identity, trust and authorization; "
              "no reconciliation attempted", file=sys.stderr)
        return 1

    rc, output = run_remote(CLIENT, ["herdr", "machine", "list", "--json"])
    catalog = parse_json(output)
    entries = [entry for entry in catalog if isinstance(entry, dict)
               and entry.get("label") == WORKLOAD] if isinstance(catalog, list) else []
    registered = expect(rc == 0 and len(entries) == 1
                        and entries[0].get("target") == WORKLOAD
                        and entries[0].get("session") == "default"
                        and entries[0].get("enabled") is True,
                        "client machine catalog contains intended enabled workload")
    if not registered:
        print("BLOCKED: managed machine registration required; no reconciliation attempted",
              file=sys.stderr)
        return 1

    rc, output = run_remote(CLIENT, [
        "ssh", "-o", "BatchMode=yes", WORKLOAD, "printenv", "XDG_CONFIG_HOME"])
    ssh_xdg_ok = expect(rc == 0 and output.strip() == EXPECTED_CONFIG_HOME,
                        "managed SSH delivers Herdr explicit-session config directory")
    results.append(ssh_xdg_ok)
    if not ssh_xdg_ok:
        print("BLOCKED: remote Herdr explicit-session config mismatch; "
              "no workspace probe attempted", file=sys.stderr)
        return 1

    rc, output = run_remote(CLIENT, ["herdr", "machine", "status", WORKLOAD, "--json"])
    status = parse_json(output)
    results.append(expect(rc == 0 and isinstance(status, list) and len(status) == 1
                          and isinstance(status[0], dict)
                          and status[0].get("label") == WORKLOAD
                          and status[0].get("status") == "reachable",
                          "targeted client machine status reports reachable"))

    rc, output = run_remote(CLIENT, ["herdr", "--machine", WORKLOAD, "workspace", "list"])
    envelope = parse_json(output)
    result = envelope.get("result") if isinstance(envelope, dict) else None
    results.append(expect(rc == 0 and isinstance(result, dict)
                          and result.get("type") == "workspace_list"
                          and isinstance(result.get("workspaces"), list),
                          "client remote workspace list via managed SSH"))

    # This deliberate negative probe is the only environment override. Positive
    # remote discovery above relies on sshd, never a per-call socket override.
    rc, output = run_remote(WORKLOAD, [
        "su", "-", "agent", "-c",
        "HERDR_SOCKET_PATH=/run/herdr/missing.sock herdr api snapshot"], merge_stderr=True)
    envelope = parse_json(output)
    error = envelope.get("error") if isinstance(envelope, dict) else None
    results.append(expect(rc not in (0, 124, 127) and isinstance(error, dict)
                          and error.get("code") == "server_not_running",
                          "missing socket rejected"))
    if not all(results):
        return 1
    print("OVERALL: PASS (existing guest only; restart/recreation not tested)")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (RuntimeError, ValueError, KeyError, TypeError):
        print("FAIL: invalid pilot probe response (output suppressed)", file=sys.stderr)
        sys.exit(1)
