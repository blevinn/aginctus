"""Read-only pilot infrastructure preflight for #124; no Herdr or inference claims."""
import json
import re
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

ROUTE_HEX = re.compile(r"[0-9A-Fa-f]{8}\Z")


def verify_ipv4_routes(data):
    """Read-only kernel route-table check; cannot prove packet filtering.

    Accept an on-link eth0 route, but never accept default routes, unknown
    interfaces or corrupt output as a successful isolation preflight.
    """
    if not isinstance(data, str) or len(data) > 65536:
        raise RuntimeError("invalid workload IPv4 route table")
    lines = data.splitlines()
    if not lines or lines[0].split() != [
        "Iface", "Destination", "Gateway", "Flags", "RefCnt",
        "Use", "Metric", "Mask", "MTU", "Window", "IRTT",
    ]:
        raise RuntimeError("invalid workload IPv4 route table")
    if len(lines) > 1024:
        raise RuntimeError("unbounded workload IPv4 route table")
    on_link = False
    for line in lines[1:]:
        fields = line.split()
        if len(fields) != 11 or fields[0] not in ("lo", "eth0"):
            raise RuntimeError("unexpected workload IPv4 route")
        if (any(not ROUTE_HEX.fullmatch(fields[i]) for i in (1, 2, 7))
                or not re.fullmatch(r"[0-9A-Fa-f]{1,8}", fields[3])):
            raise RuntimeError("malformed workload IPv4 route")
        if any(not number.isdecimal() for number in (
            fields[4], fields[5], fields[6], fields[8], fields[9], fields[10]
        )):
            raise RuntimeError("malformed workload IPv4 route")
        destination, gateway, mask = fields[1], fields[2], fields[7]
        if destination == "00000000" and mask == "00000000":
            raise RuntimeError("workload has an IPv4 default route")
        if fields[0] == "eth0" and gateway == "00000000":
            on_link = True
    if not on_link:
        raise RuntimeError("workload management route is not observed")


def verify_managed_workload(record, network):
    config = record.get("config")
    devices = record.get("expanded_devices")
    if not isinstance(config, dict) or any(config.get(k) != v for k, v in {
        "user.aginctus.managed": "true",
        "user.aginctus.resource": "workload",
        "user.aginctus.role": "agent-workload",
        "user.aginctus.workload": "dev",
    }.items()):
        raise RuntimeError("workload ownership not verified")
    if not isinstance(devices, dict) or not devices:
        raise RuntimeError("workload effective NIC inventory unavailable")
    nics = [device for device in devices.values()
            if isinstance(device, dict) and device.get("type") == "nic"]
    if (len(nics) != 1 or nics[0].get("network") != network
            or nics[0].get("name") != "eth0"):
        raise RuntimeError("workload has an unexpected network attachment")
    # Reject proxy, physical and other devices capable of exposing
    # alternate network paths; only disk and the reviewed NIC are expected.
    if any(not isinstance(device, dict) or device.get("type") not in ("disk", "nic")
           for device in devices.values()):
        raise RuntimeError("workload has an unsafe non-NIC attachment")


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
        if instance == workload:
            # Do not execute even a read-only guest command until ownership,
            # effective NIC inventory and exact expected bridge are checked.
            verify_managed_workload(record, network)
    route_table = run(
        "incus", "--project", "default", "exec", workload, "--",
        "cat", "/proc/net/route",
    )
    verify_ipv4_routes(route_table)
    print("PASS: read-only Incus bridge, managed workload NIC and IPv4 route preflight")
    print("NOT VERIFIED: actual packet denial, IPv6/tunnels, gateway inference, guest credential isolation or provider egress")

if __name__ == "__main__":
    if len(sys.argv) != 1:
        sys.exit("usage: opencode-pilot-preflight.py")
    try:
        verify()
    except (RuntimeError, ValueError, KeyError, json.JSONDecodeError) as error:
        # Never emit command output or endpoint values: they may contain secrets.
        print("FAIL: pilot infrastructure preflight (" + str(error) + ")", file=sys.stderr)
        sys.exit(1)
