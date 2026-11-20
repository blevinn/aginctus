"""Operator-only host-observed guest TCP controls; NEVER full egress proof.

An approved, listening public IPv4 endpoint is required for the host baseline.
A guest ENETUNREACH is a point-in-time route observation, not a firewall
attestation. No credentials, DNS, HTTP, TLS or model API calls are made.
"""

import argparse
import contextlib
import importlib.util
import io
import ipaddress
import json
import os
import selectors
import socket
import subprocess
import sys
import time
from pathlib import Path

GUEST_PROGRAM = "/run/current-system/sw/bin/aginctus-egress-probe"
GUEST_CONFIG = "/var/lib/aginctus/opencode/gateway.json"
CHECKS = (
    "preflight/managed_topology",
    "guest/configured_gateway",
    "guest/gateway_tcp",
    "host/public_tcp_control",
    "guest/direct_public_no_route",
)


class ProbeError(Exception):
    pass


def load_preflight():
    path = Path(__file__).with_name("opencode-pilot-preflight.py")
    spec = importlib.util.spec_from_file_location("aginctus_pilot_preflight", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def target(mode, value):
    try:
        ip = ipaddress.IPv4Address(value)
    except (ValueError, TypeError):
        raise ProbeError("invalid IPv4 selector") from None
    if str(ip) != value:
        raise ProbeError("noncanonical IPv4 selector")
    private = any(ip in ipaddress.ip_network(net) for net in (
        "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
    ))
    if (mode == "gateway" and not private) or (mode == "public" and not ip.is_global):
        raise ProbeError("unexpected IPv4 target classification")
    return str(ip)


def execute(argv):
    """Bound actual subprocess stdout allocation, elapsed time and output."""
    try:
        with subprocess.Popen(
            argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL, shell=False,
        ) as process:
            try:
                limit = 4096
                deadline = time.monotonic() + 20
                output = bytearray()
                with selectors.DefaultSelector() as selector:
                    selector.register(process.stdout, selectors.EVENT_READ)
                    while True:
                        remaining = deadline - time.monotonic()
                        if remaining <= 0 or not selector.select(remaining):
                            raise ProbeError("guest observation timed out")
                        chunk = os.read(
                            process.stdout.fileno(), min(4096, limit + 1 - len(output))
                        )
                        output.extend(chunk)
                        if len(output) > limit:
                            raise ProbeError("oversized guest observation")
                        if not chunk:
                            break
                code = process.wait(timeout=max(0.001, deadline - time.monotonic()))
                if code not in (0, 1):
                    raise ProbeError("invalid guest observation exit")
                return subprocess.CompletedProcess(
                    argv, code, output.decode("utf-8"), "",
                )
            finally:
                if process.poll() is None:
                    process.kill()
                process.wait()
    except (OSError, UnicodeDecodeError, subprocess.SubprocessError):
        raise ProbeError("read-only guest execution unavailable") from None


def guest_args(workload, *tail):
    return [
        "incus", "--project", "default", "exec", workload, "--",
        "runuser", "-u", "agent", "--", *tail,
    ]


def unique_object(pairs):
    record = {}
    for key, value in pairs:
        if key in record:
            raise ValueError("duplicate guest JSON field")
        record[key] = value
    return record


def configured_gateway(workload, gateway_ip):
    result = execute(guest_args(workload, "cat", GUEST_CONFIG))
    if result.returncode != 0:
        raise ProbeError("guest gateway configuration unavailable")
    try:
        document = json.loads(result.stdout, object_pairs_hook=unique_object)
    except (ValueError, UnicodeError):
        raise ProbeError("invalid guest gateway configuration") from None
    if (
        not isinstance(document, dict)
        or set(document) != {"endpoint", "model", "protocol"}
        or document["endpoint"] != f"http://{gateway_ip}:4000/v1"
        or document["protocol"] != "responses"
        or not isinstance(document["model"], str)
        or not document["model"]
    ):
        raise ProbeError("guest gateway does not match reviewed endpoint")


def guest_probe(workload, mode, ip):
    check = {"gateway": "gateway_tcp", "direct": "direct_public_no_route"}[mode]
    result = execute(guest_args(workload, GUEST_PROGRAM, mode, ip))
    try:
        record = json.loads(result.stdout, object_pairs_hook=unique_object)
    except (ValueError, UnicodeError):
        raise ProbeError("invalid guest probe response") from None
    if (
        not isinstance(record, dict)
        or set(record) != {"schema_version", "profile", "check", "status"}
        or type(record["schema_version"]) is not int
        or record["schema_version"] != 1
        or record["profile"] != "guest_tcp_probe"
        or record["check"] != check
        or record["status"] not in ("pass", "fail", "error")
        or (result.returncode == 0) != (record["status"] == "pass")
    ):
        raise ProbeError("contradictory guest probe result")
    return record["status"]


def host_control(ip, connector=None):
    # Resolve only at call time, so hostless mocks cannot accidentally bypass
    # the mocked socket and contact an external endpoint.
    connector = connector or socket.create_connection
    # The operator-selected endpoint must be independently known to listen.
    # Host socket success is a necessary control, not a guarantee the remote
    # server also accepts connections from a distinct guest source address.
    try:
        with connector((ip, 443), timeout=3):
            return "pass"
    except OSError:
        return "error"


def report(statuses):
    rows = [{"id": name, "status": statuses[name]} for name in CHECKS]
    if "error" in statuses.values():
        outcome = "error"
    elif "fail" in statuses.values():
        outcome = "fail"
    else:
        outcome = "incomplete"
    return {
        "schema_version": 1,
        "profile": "guest_network_controls",
        "status": outcome,
        "results": rows,
        "credential_egress": "not_verified",
        "upstream_inference": "not_verified",
        "complete_acceptance": False,
    }


def inspect(gateway_ip, public_ip, preflight=None):
    preflight = preflight or load_preflight()
    statuses = dict.fromkeys(CHECKS, "not_run")
    try:
        # The preflight must run before guest network execution. It checks
        # Aginctus ownership, single management NIC and no IPv4 default route.
        with contextlib.redirect_stdout(io.StringIO()):
            preflight.verify()
        statuses[CHECKS[0]] = "pass"
        workload = preflight.config("workloads.dev.instance.name")
        configured_gateway(workload, gateway_ip)
        statuses[CHECKS[1]] = "pass"
        statuses[CHECKS[2]] = guest_probe(workload, "gateway", gateway_ip)
        if statuses[CHECKS[2]] != "pass":
            return report(statuses)
        statuses[CHECKS[3]] = host_control(public_ip)
        if statuses[CHECKS[3]] != "pass":
            return report(statuses)
        statuses[CHECKS[4]] = guest_probe(workload, "direct", public_ip)
    except (ProbeError, RuntimeError, KeyError, OSError, ValueError):
        next_unrun = next((item for item in CHECKS if statuses[item] == "not_run"), None)
        if next_unrun is not None:
            statuses[next_unrun] = "error"
    return report(statuses)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway-ip", required=True)
    parser.add_argument("--public-ip", required=True)
    parser.add_argument("--run", action="store_true", help="Explicit operator-only TCP observation")
    args = parser.parse_args(argv)
    statuses = dict.fromkeys(CHECKS, "not_run")
    try:
        gateway = target("gateway", args.gateway_ip)
        public = target("public", args.public_ip)
        if not args.run:
            result = report(statuses)
            print(json.dumps(result, separators=(",", ":")))
            return 0
        if os.environ.get("AGINCTUS_ACCEPTANCE_EGRESS") != "I_UNDERSTAND":
            raise ProbeError("operator approval missing")
        result = inspect(gateway, public)
    except ProbeError:
        statuses[CHECKS[0]] = "error"
        result = report(statuses)
    print(json.dumps(result, separators=(",", ":")))
    return 0 if all(r["status"] == "pass" for r in result["results"]) else 1


if __name__ == "__main__":
    sys.exit(main())
