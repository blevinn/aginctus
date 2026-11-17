"""Fixed, non-credential TCP observation executed as unprivileged agent.

A public connect result of ENETUNREACH means ONLY that this guest socket
had no IPv4 route to the selected address at the time of the attempt.
No HTTP, DNS, TLS, prompts, bearer keys, or provider APIs are used.
"""

import errno
import ipaddress
import json
import socket
import sys

PRIVATE = tuple(ipaddress.ip_network(value) for value in (
    "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
))


def valid_target(mode, raw):
    try:
        address = ipaddress.IPv4Address(raw)
    except (ValueError, ipaddress.AddressValueError):
        raise ValueError("invalid IPv4 address") from None
    if str(address) != raw:
        raise ValueError("noncanonical IPv4 address")
    if mode == "gateway" and not any(address in item for item in PRIVATE):
        raise ValueError("expected private gateway IPv4")
    if mode == "direct" and not address.is_global:
        raise ValueError("expected global IPv4")
    if mode not in ("gateway", "direct"):
        raise ValueError("unknown TCP probe mode")
    return str(address), 4000 if mode == "gateway" else 443


def observe(mode, raw, connector=socket.create_connection):
    address, port = valid_target(mode, raw)
    try:
        with connector((address, port), timeout=3):
            pass
    except OSError as exc:
        if mode == "direct" and exc.errno == errno.ENETUNREACH:
            # Narrow positive result: the guest kernel reported no IPv4 route.
            return "pass"
        # Refused connections, timeouts, unknown errors and firewall drops
        # are NOT proof of no route / blocked outbound egress.
        return "error"
    return "pass" if mode == "gateway" else "fail"


def main(argv=None):
    args = sys.argv[1:] if argv is None else argv
    mode = args[0] if len(args) == 2 else ""
    status = "error"
    try:
        if len(args) == 2:
            status = observe(mode, args[1])
    except (OSError, ValueError, TypeError):
        pass
    check = {"gateway": "gateway_tcp", "direct": "direct_public_no_route"}.get(
        mode, "invalid"
    )
    print(json.dumps({
        "schema_version": 1,
        "profile": "guest_tcp_probe",
        "check": check,
        "status": status,
    }, separators=(",", ":")))
    return 0 if status == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
