# Read-only OpenCode pilot infrastructure preflight (#124)

This increment implements **actual read-only Incus interrogation**, not the complete live acceptance adapter. It does not execute a model request, discover the Herdr automation API, modify firewall rules or claim that gateway egress was proven.

Run on a provisioned NixOS pilot with `aginctus` and `incus` in PATH, from the repository development shell (`nix develop`, which includes Python 3):

```sh
python3 scripts/acceptance/opencode-pilot-preflight.py
```

The script retrieves effective resource names using `aginctus config get` and reads the default Incus project's network/instance resources using `incus query`. It asserts that the configured management bridge is a bridge with explicit `ipv4.nat=false`, `ipv4.routing=false` and `ipv6.address=none`; the configured Herdr client and `dev` OpenCode workload must be running containers. Query timeouts, missing resources, malformed configuration and unsafe settings fail closed. It does not print queried JSON or runtime secrets.

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

This inspection does **not** prove that the containers are attached to the expected bridge, that no additional interfaces exist, that firewall policy permits DNS/DHCP, that upstream egress is denied or that Herdr has authenticated. These require deeper checks before live acceptance. In particular, do not adopt this script as the `preflight` hook unmodified: a full hook must also enforce the conditions in [live adapter requirements](acceptance-opencode-live.md).

Deterministic validation (no Incus required; run from `nix develop`):

```sh
python3 -m unittest discover -s scripts/acceptance -p 'test_opencode_pilot_preflight.py' -v
python3 -m py_compile scripts/acceptance/opencode-pilot-preflight.py
bash scripts/acceptance/opencode-live-adapter-test.sh
bash scripts/acceptance/opencode-test.sh
git diff --check origin/main...HEAD
just check
```

The VM/host pilot verification can run independently from #123 while its launcher remains under development. For a complete #118 sign-off, follow this inspection with real Herdr launch/prompt/wait and gateway-side request correlation; no synthetic marker or test double counts as live evidence.
