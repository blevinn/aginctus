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


def incus(*args, input_data=None):
    return subprocess.run(
        ["incus", *args], input=input_data, text=True,
        capture_output=True, check=True, timeout=120,
    )


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
    try:
        # No profiles and no network device: cannot contact a model provider.
        incus("init", opts.image, name, "--project", project,
              "--no-profiles", "--storage", opts.pool)
        created = True
        incus("start", name, "--project", project)
        incus("exec", name, "--project", project, "--",
              "aginctus-opencode-gateway-provision", input_data=payload)

        # No credential value is read back or displayed. The shell script has
        # fixed text, contains no secrets, and checks metadata and safe config.
        verification = r'''
set -eu
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
              "sh", "-c", verification)

        # Verify invalid payload cannot replace a successfully installed key.
        bad_payload = json.dumps({
            "endpoint": "http://10.42.40.12:4000/v1",
            "model": "invalid/model",
            "key": "wrong-key",
        })
        bad = subprocess.run(
            ["incus", "exec", name, "--project", project, "--",
             "aginctus-opencode-gateway-provision"],
            input=bad_payload, text=True, capture_output=True, timeout=120,
        )
        if bad.returncode == 0:
            raise RuntimeError("guest accepted invalid model")
        incus("exec", name, "--project", project, "--", "sh", "-c", verification)
        print("PASS: disposable offline Incus guest accepted stdin provisioning, "
              "enforced private files, and rejected invalid input")
    finally:
        if created:
            try:
                incus("delete", name, "--project", project, "--force")
            except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
                print("WARNING: disposable test instance cleanup failed: " + name,
                      file=sys.stderr)
                raise


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
        # Never include process stderr, which might echo stdin on failures.
        sys.exit("FAIL: Incus command failed; inspect instance status without printing secrets")
