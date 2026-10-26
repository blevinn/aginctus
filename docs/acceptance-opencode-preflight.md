# Read-only OpenCode pilot infrastructure preflight (#124)

This is the **first read-only check in stage 2** of the [OpenCode acceptance decision path](acceptance-opencode.md#2-read-only-pilot-inspection). Follow it with the separately documented [guest gateway readiness preflight](acceptance-opencode-inference.md) before considering any opted-in Herdr or live-provider test. Neither preflight provides authorization to alter Incus or the host.

This increment implements **actual read-only Incus interrogation**, not the complete live acceptance adapter. It does not execute a model request, discover the Herdr automation API, modify firewall rules or claim that gateway egress was proven.

Run on a provisioned NixOS pilot with `aginctus` and `incus` in PATH, from the repository development shell (`nix develop`, which includes Python 3):

```sh
python3 scripts/acceptance/opencode-pilot-preflight.py
```

The script retrieves effective resource names using `aginctus config get` and reads the default Incus project's network/instance resources using `incus query`. It asserts that the configured management bridge is a bridge with explicit `ipv4.nat=false`, `ipv4.routing=false` and `ipv6.address=none`; the configured Herdr client and `dev` OpenCode workload must be running containers.

As an additional #521 read-only network-topology observation, it now checks the **effective** OpenCode workload devices (including profile-expanded devices) and requires exactly one NIC, named `eth0`, attached to the selected management bridge, with no Incus proxy devices. The workload must carry the expected Aginctus-managed ownership metadata before the host runs any guest command. It then reads `/proc/net/route` from that same owned guest via a fixed `incus --project default exec ... -- cat` invocation; malformed tables, unexpected interfaces, missing on-link `eth0` routes or any IPv4 default route fail closed. This query reads Linux kernel state, makes no network requests and does not mutate a network or a workload.

Query timeouts, missing resources, malformed configuration, unexpected NICs/routes and unsafe settings fail closed. It never prints queried route tables, guest command output, secrets or raw errors.

### Diagnosing a failed read-only command

The Nix development shell provides Python and Incus, but **does not install the Aginctus CLI**. If `aginctus` is not already installed, build it separately and put it on PATH for this invocation. These checks do not change Incus resources:

```sh
nix develop -c bash -c 'command -v aginctus || echo "Aginctus CLI not on PATH"; command -v incus; incus info >/dev/null && echo "Incus reachable"'
# From a checkout, build the CLI without installing it on the host:
tmp="$(mktemp -d)"
nix develop -c go build -o "$tmp/aginctus" ./cmd/aginctus
nix develop -c bash -c 'PATH="$1:$PATH" python3 scripts/acceptance/opencode-pilot-preflight.py' bash "$tmp"
rm -rf -- "$tmp"
```

If the Aginctus CLI is already installed, no build is necessary. The temporary binary is only a local development convenience. Do not print Incus query output or runtime credentials. The preflight now distinguishes a missing executable from a failing read-only config/query operation without exposing command stderr.

This inspection now checks the selected OpenCode guest's Incus **effective network-device inventory** and observed IPv4 route table, but it does **not** establish actual negative egress enforcement: a host can forward packets unexpectedly, explicit non-default routes and overlay/tunnel paths can exist, and the guest may use other address families or process-level proxies. It does not prove DNS/DHCP/host firewall enforcement, gateway reachability, upstream Responses completion or Herdr authentication. Guest-originated positive/negative egress probes and host firewall observations remain separately required before live acceptance. In particular, do not adopt this script as the `preflight` hook unmodified: a full hook must also enforce the conditions in [live adapter requirements](acceptance-opencode-live.md).

Deterministic validation (no Incus required; run from `nix develop`):

```sh
python3 -m unittest discover -s scripts/acceptance -p 'test_opencode_pilot_preflight.py' -v
python3 -m py_compile scripts/acceptance/opencode-pilot-preflight.py
# Hostless tests now also exercise effective NIC and malformed/default route cases.
bash scripts/acceptance/opencode-live-adapter-test.sh
bash scripts/acceptance/opencode-test.sh
git diff --check origin/main...HEAD
just check
```

This read-only topology inspection can run independently of any live-agent invocation. For a complete #118 sign-off, subsequent **separately authorized** Herdr launch/prompt/wait, actual agent tool traces, direct-egress denial and gateway-side successful upstream request correlation remain necessary. No synthetic marker, health check or test double counts as live inference evidence.
