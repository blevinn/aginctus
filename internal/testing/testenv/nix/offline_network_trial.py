"""Fixed, synthetic IPv4 TCP/UDP probes for the no-NIC disposable guest.

The listener and its positive-control route exist only inside the outer
test VM. No real upstream or caller-host endpoint is used.
"""

import json as network_json
import re as network_re
import shlex as network_shlex

NETWORK_PROBE = "network-probe"
NETWORK_NS = "te-netprobe"
NETWORK_UNIT = "aginctus-testenv-network-probe"
NETWORK_PEER = "192.0.2.2"
NETWORK_PORT = 18937

NETWORK_SERVER = """import selectors
import socket
sel = selectors.DefaultSelector()
address = ("192.0.2.2", 18937)
tcp = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
tcp.bind(address)
tcp.listen(4)
udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
udp.bind(address)
sel.register(tcp, selectors.EVENT_READ, "tcp")
sel.register(udp, selectors.EVENT_READ, "udp")
while True:
    for key, _ in sel.select():
        if key.data == "tcp":
            conn, _ = tcp.accept()
            with conn:
                conn.settimeout(2)
                print("PROBE tcp", flush=True)
                conn.sendall(b"testenv")
        else:
            data, peer = udp.recvfrom(64)
            print("PROBE udp", flush=True)
            udp.sendto(b"testenv", peer)
"""

NETWORK_POSITIVE = """import socket
address = ("192.0.2.2", 18937)
with socket.create_connection(address, timeout=2) as conn:
    with conn.makefile("rb") as response:
        assert response.read(7) == b"testenv"
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as conn:
    conn.settimeout(2)
    conn.connect(address)
    conn.send(b"testenv")
    assert conn.recv(64) == b"testenv"
print("positive")
"""

NETWORK_NEGATIVE = """import errno
import socket
# Positive local controls: the same guest stack can exchange TCP/UDP.
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as server:
    server.bind(("127.0.0.1", 0))
    server.listen(1)
    server.settimeout(2)
    with socket.create_connection(server.getsockname(), timeout=2) as client:
        conn, _ = server.accept()
        with conn:
            conn.sendall(b"local")
            with client.makefile("rb") as response:
                assert response.read(5) == b"local"
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as server:
    server.bind(("127.0.0.1", 0))
    server.settimeout(2)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
        client.sendto(b"local", server.getsockname())
        assert server.recv(64) == b"local"
address = ("192.0.2.2", 18937)
allowed = (errno.ENETUNREACH, errno.EHOSTUNREACH)
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as conn:
    conn.settimeout(2)
    try:
        conn.connect(address)
    except TimeoutError:
        pass
    except OSError as err:
        if err.errno not in allowed:
            raise
    else:
        raise RuntimeError("synthetic TCP endpoint reachable")
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as conn:
    conn.settimeout(2)
    try:
        conn.connect(address)
        conn.send(b"testenv")
        conn.recv(64)
    except TimeoutError:
        pass
    except OSError as err:
        if err.errno not in allowed:
            raise
    else:
        raise RuntimeError("synthetic UDP endpoint reachable")
print("negative")
"""


def _network_command(*args):
    return network_shlex.join(args)


def _network_marker(machine, command, marker):
    actual = machine.succeed(command, timeout=20).strip()
    if actual != marker:
        raise RuntimeError("network probe marker mismatch")


def _network_count(machine):
    # The listener logs exactly one marker per accepted TCP or UDP request.
    output = machine.succeed(
        "journalctl -b -u " + NETWORK_UNIT + ".service --no-pager -o cat "
        "| awk '/^PROBE (tcp|udp)$/ {n++} END {print n+0}'"
    ).strip()
    if not network_re.fullmatch(r"[0-9]{1,6}", output):
        raise RuntimeError("network endpoint count unavailable")
    return int(output)


def run_offline_network(machine, fingerprint, inventory, cleanup, project_prefix, sentinel, python, ip, nft):
    if not network_re.fullmatch(r"[0-9a-f]{64}", fingerprint):
        raise RuntimeError("network probe requires the pinned image fingerprint")
    baseline = inventory(machine)
    if baseline[0] != [(sentinel, "Stopped")]:
        raise RuntimeError("network probe requires only the stopped foreign sentinel")
    # A dedicated point-to-point worker-local segment, with no uplink and no
    # namespace default route. No forwarding rules or host firewall changes.
    machine.succeed(_network_command(ip, "netns", "add", NETWORK_NS))
    machine.succeed(_network_command(ip, "link", "add", "te-probe-w", "type", "veth", "peer", "name", "te-probe-p"))
    machine.succeed(_network_command(ip, "link", "set", "te-probe-p", "netns", NETWORK_NS))
    machine.succeed(_network_command(ip, "addr", "add", "192.0.2.1/30", "dev", "te-probe-w"))
    machine.succeed(_network_command(ip, "link", "set", "te-probe-w", "up"))
    ns = [ip, "netns", "exec", NETWORK_NS, ip]
    machine.succeed(_network_command(*ns, "link", "set", "lo", "up"))
    machine.succeed(_network_command(*ns, "addr", "add", "192.0.2.2/30", "dev", "te-probe-p"))
    machine.succeed(_network_command(*ns, "link", "set", "te-probe-p", "up"))
    if machine.succeed(_network_command(*ns, "-4", "route", "show", "default")).strip():
        raise RuntimeError("synthetic endpoint has an unexpected default route")
    machine.succeed(_network_command(
        "systemd-run", "--unit=" + NETWORK_UNIT, "--service-type=exec",
        "--property=RuntimeMaxSec=10min", ip, "netns", "exec", NETWORK_NS,
        python, "-I", "-u", "-c", NETWORK_SERVER,
    ))
    positive = _network_command(python, "-I", "-c", NETWORK_POSITIVE)
    machine.wait_until_succeeds(positive, timeout=30)
    _network_marker(machine, positive, "positive")
    prefix = project_prefix + " "
    machine.succeed(prefix + _network_command(
        "init", fingerprint, NETWORK_PROBE, "--vm", "--no-profiles", "--storage", "testenv",
        "-c", "limits.cpu=2", "-c", "limits.memory=4096MiB", "-c", "security.nesting=true",
        "-d", "root,size=32GiB",
    ), timeout=240)
    machine.succeed(prefix + "start " + NETWORK_PROBE, timeout=120)
    execute = "timeout 15 " + prefix + "exec --disable-stdin " + NETWORK_PROBE + " -- "
    machine.wait_until_succeeds(execute + "/run/current-system/sw/bin/true", timeout=120)

    def check_links():
        links = network_json.loads(machine.succeed(execute + _network_command(ip, "-j", "link", "show")))
        if [link["ifname"] for link in links] != ["lo"]:
            raise RuntimeError("offline guest has an unexpected network interface")

    check_links()
    if machine.succeed(execute + "/run/current-system/sw/bin/id -u").strip() != "0":
        raise RuntimeError("guest root firewall control unverified")
    count = _network_count(machine)
    if count < 2:
        raise RuntimeError("synthetic endpoint positive control unverified")
    negative = execute + _network_command(python, "-I", "-c", NETWORK_NEGATIVE)
    _network_marker(machine, negative, "negative")
    print("FIXTURE OFFLINE NETWORK: TCP/UDP denied before guest firewall change")
    # Deliberate root mutation is confined to the fixed synthetic inner VM.
    machine.succeed(execute + _network_command(nft, "add", "table", "inet", "testenv_probe"))
    machine.succeed(execute + _network_command(nft, "list", "table", "inet", "testenv_probe"))
    machine.succeed(execute + _network_command(nft, "flush", "ruleset"))
    if machine.succeed(execute + _network_command(nft, "list", "ruleset")).strip():
        raise RuntimeError("guest firewall removal unverified")
    check_links()
    _network_marker(machine, negative, "negative")
    if _network_count(machine) != count:
        raise RuntimeError("synthetic endpoint observed unexpected guest traffic")
    _network_marker(machine, positive, "positive")
    if _network_count(machine) != count + 2:
        raise RuntimeError("post-probe positive endpoint control unverified")
    print("FIXTURE OFFLINE NETWORK: TCP/UDP denied after guest firewall removal")
    machine.succeed(prefix + "stop " + NETWORK_PROBE + " --force", timeout=120)
    machine.succeed(prefix + "delete " + NETWORK_PROBE, timeout=120)
    machine.succeed("systemctl stop " + NETWORK_UNIT + ".service")
    machine.succeed(_network_command(ip, "link", "delete", "te-probe-w"))
    machine.succeed(_network_command(ip, "netns", "delete", NETWORK_NS))
    cleanup(baseline, inventory(machine), fingerprint)
    return [
        {"id": "offline_tcp_udp_boundary", "status": "pass"},
        {"id": "guest_firewall_independence", "status": "pass"},
    ]
