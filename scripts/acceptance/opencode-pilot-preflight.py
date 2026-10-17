#!/usr/bin/env python3
"""Read-only pilot infrastructure preflight for #124; no Herdr or inference claims."""
import json
import subprocess
import sys
from urllib.parse import quote

def run(*args):
    try:
        result = subprocess.run(args, capture_output=True, text=True, timeout=15, check=True)
        return result.stdout.strip()
    except subprocess.TimeoutExpired:
        raise RuntimeError("read-only command timed out: " + " ".join(args[:3])) from None
    except FileNotFoundError:
        raise RuntimeError("required executable missing: " + args[0]) from None
    except subprocess.CalledProcessError:
        # Never include stderr or stdout; only approved static operation labels.
        if args[0] == "aginctus":
            label = "config get"
        elif args[0] == "incus":
            label = "query"
        else:
            label = "command"
        raise RuntimeError("read-only " + label + " failed") from None
    except OSError:
        raise RuntimeError("unable to invoke required read-only command") from None

def config(name):
    value = run("aginctus", "config", "get", name)
    try:
        value = json.loads(value)
    except json.JSONDecodeError:
        value = value.strip('"')
    if not isinstance(value, str) or not value or "/" in value or value in (".", ".."):
        raise RuntimeError("invalid configured resource name")
    return value

def resource(kind, name):
    # The pilot deployment uses the Incus default project for managed workloads.
    path = "/1.0/" + kind + "/" + quote(name, safe="") + "?project=default"
    result = json.loads(run("incus", "query", path))
    if isinstance(result, dict) and "metadata" in result:
        result = result["metadata"]
    if not isinstance(result, dict):
        raise RuntimeError("invalid Incus resource response")
    return result

def verify():
    network = config("incus.management.network.name")
    herdr = config("infrastructure.herdr.name")
    workload = config("workloads.dev.instance.name")
    bridge = resource("networks", network)
    settings = bridge.get("config", {})
    if bridge.get("type") != "bridge":
        raise RuntimeError("management network is not an Incus bridge")
    if settings.get("ipv4.nat") != "false" or settings.get("ipv4.routing") != "false":
        raise RuntimeError("management network NAT/routing is not explicitly disabled")
    if settings.get("ipv6.address") != "none":
        raise RuntimeError("management network IPv6 is not disabled")
    for instance in (herdr, workload):
        record = resource("instances", instance)
        if record.get("status") != "Running":
            raise RuntimeError("required managed instance is not running")
        if record.get("type") != "container":
            raise RuntimeError("required managed instance is not a container")
    print("PASS: read-only Incus bridge isolation and instance readiness")
    print("NOT VERIFIED: Herdr authentication, workspace mapping, DNS/DHCP, gateway inference, credential isolation, and egress policy")

if __name__ == "__main__":
    if len(sys.argv) != 1:
        sys.exit("usage: opencode-pilot-preflight.py")
    try:
        verify()
    except (RuntimeError, ValueError, KeyError, json.JSONDecodeError) as error:
        # Never emit command output or endpoint values: they may contain secrets.
        print("FAIL: pilot infrastructure preflight (" + str(error) + ")", file=sys.stderr)
        sys.exit(1)
