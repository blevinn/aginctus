#!/usr/bin/env python3
"""Live Incus guest-helper smoke test, independent of LiteLLM.

Creates and destroys *only* its own disposable offline OpenCode instance.
Never runs the production gateway provisioning command or touches aginctus-dev.
"""
import argparse
import json
import secrets
import subprocess
import sys
import uuid


def incus(operation, *args, input_data=None, stage=None):
    try:
        return subprocess.run(
            ["incus", operation, *args], input=input_data, text=True,
            capture_output=True, check=True, timeout=120,
        )
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as exc:
        # The tool's stderr may contain credential-bearing stdin. Report only
        # the operation and exit code, never raw command output or payload.
        code = getattr(exc, "returncode", "timeout")
        raise RuntimeError(f"{stage or operation} failed (status {code})") from None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--image", default="aginctus-opencode-workload")
    parser.add_argument("--pool", required=True)
    opts = parser.parse_args()

    name = "aginctus-provision-test-" + uuid.uuid4().hex[:12]
    project = "default"
    created = False
    key = "disposable-" + secrets.token_urlsafe(32)
    payload = json.dumps({
        "endpoint": "http://10.42.40.12:4000/v1",
        "model": "mock-alias",
        "key": key,
    })
    helper = "/run/current-system/sw/bin/aginctus-opencode-gateway-provision"
    try:
        # No profiles and no network device: cannot contact a model provider.
        incus("init", opts.image, name, "--project", project,
              "--no-profiles", "--storage", opts.pool)
        created = True
        incus("start", name, "--project", project)
        # Probe the imported image *before* passing even a disposable key.
        # Use a fixed, nonsecret script; never echo file contents.
        probe = """set -eu
for path in /run/current-system/sw/bin/aginctus-opencode-gateway-provision /nix/var/nix/profiles/system/sw/bin/aginctus-opencode-gateway-provision /run/current-system /nix/var/nix/profiles/system; do
  if [ -e "$path" ]; then printf 'EXISTS %s\\n' "$path"; else printf 'MISSING %s\\n' "$path"; fi
done
"""
        result = incus("exec", name, "--project", project, "--",
                       "/bin/sh", "-c", probe, stage="guest image profile diagnostic")
        print(result.stdout, end="")
        if "EXISTS /run/current-system/sw/bin/aginctus-opencode-gateway-provision" not in result.stdout:
            raise RuntimeError("guest helper absent at expected system profile path")

        incus("exec", name, "--project", project, "--",
              helper, input_data=payload, stage="guest helper provisioning")

        # No credential value is read back or displayed. The shell script has
        # fixed text, contains no secrets, and checks metadata and safe config.
        verification = r'''
set -eu
PATH=/run/current-system/sw/bin:/usr/bin:/bin
export PATH
dir=/var/lib/aginctus/opencode
test "$(stat -c %a "$dir")" = 700
test "$(stat -c %a "$dir/workload-key")" = 600
test "$(stat -c %a "$dir/gateway.json")" = 600
test "$(stat -c %U "$dir/workload-key")" = agent
test "$(stat -c %U "$dir/gateway.json")" = agent
runuser -u agent -- test -r "$dir/workload-key"
runuser -u agent -- test -r "$dir/gateway.json"
grep -Fq '"model": "mock-alias"' "$dir/gateway.json"
grep -Fq '"protocol": "responses"' "$dir/gateway.json"
grep -Fq '"endpoint": "http://10.42.40.12:4000/v1"' "$dir/gateway.json"
'''
        incus("exec", name, "--project", project, "--",
              "/run/current-system/sw/bin/sh", "-c", verification, stage="guest file verification (initial)")

        # Verify invalid payload cannot replace a successfully installed key.
        bad_payload = json.dumps({
            "endpoint": "http://10.42.40.12:4000/v1",
            "model": "invalid/model",
            "key": "wrong-key",
        })
        bad = subprocess.run(
            ["incus", "exec", name, "--project", project, "--",
             helper],
            input=bad_payload, text=True, capture_output=True, timeout=120,
        )
        if bad.returncode == 0:
            raise RuntimeError("guest accepted invalid model")
        incus("exec", name, "--project", project, "--", "/run/current-system/sw/bin/sh", "-c", verification, stage="guest file verification (after rejection)")
        print("PASS: disposable offline Incus guest accepted stdin provisioning, "
              "enforced private files, and rejected invalid input")
    finally:
        if created:
            try:
                incus("delete", name, "--project", project, "--force")
            except RuntimeError:
                print("WARNING: disposable test instance cleanup failed: " + name,
                      file=sys.stderr)
                raise


if __name__ == "__main__":
    try:
        main()
    except RuntimeError as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        sys.exit(1)
