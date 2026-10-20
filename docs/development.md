# Development

Aginctus uses Nix to provide a small, reproducible development environment.

## Prerequisites

You need:

- Nix with flakes enabled;
- a Linux host capable of running Incus for integration work;
- an initialized Incus daemon when exercising Incus-backed functionality.

The development shell supplies the Incus client and repository tooling. It does not install or manage the host's Incus daemon.

## Enter the development shell

Run:

```sh
nix develop
```

The initial shell includes:

- `git`;
- `go`;
- `herdr` (the pinned CLI used by the OpenCode acceptance harness and the Herdr remote branches);
- `incus`;
- `jq`;
- `just`;
- `nixfmt`;
- `python3` (the guest provisioning / acceptance scripts use it directly);
- `shellcheck`;
- `shfmt`.

The flake follows the stable `nixos-26.05` Nixpkgs branch. A committed `flake.lock` should record the exact revision used by the repository.

## CLI development

The initial `aginctus` CLI is written in Go. It uses the official Incus Go client to communicate directly with the local daemon API.

Run it directly with:

```sh
go run ./cmd/aginctus help
go run ./cmd/aginctus doctor
```

The `doctor` command connects directly to the local Incus daemon, reports its server version, and checks that `core.https_address` is an explicit **numeric loopback IP with a TCP port** (for example, `127.0.0.1:8443` or `[::1]:8443`). Wildcard, LAN, DNS-hostname, malformed, or missing listeners are not reported as healthy. Plain `doctor` never modifies Incus.

Use `doctor --fix` only when the HTTPS address is **absent**: it binds `127.0.0.1:8443` for Incus image caching. If a listener is already configured to an unsafe or unrecognized address, including when `--fix` is given, Aginctus reports an error **without modifying** it; inspect the operator's connections and change `core.https_address` explicitly if appropriate. The check validates the configured bind value, not a live socket reachability probe or firewall exposure audit.

The first mutating command is:

```sh
go run ./cmd/aginctus network ensure
```

It creates or reconciles the configured management bridge. Network name, addressing, NAT, routing, and IPv6 behavior come from the effective configuration; Aginctus ownership metadata remains an internal safety invariant.

Use `--dry-run` to inspect the action without mutating Incus:

```sh
go run ./cmd/aginctus network ensure --dry-run
go run ./cmd/aginctus network teardown --dry-run
```

Use `network teardown` to remove the configured management bridge:

```sh
go run ./cmd/aginctus network teardown
```

Both `ensure` and `teardown` accept `--force`. Without it, Aginctus refuses to mutate a same-named bridge that lacks Aginctus ownership metadata. With `--force`, Aginctus may adopt or delete that same-named Incus-managed bridge. `--force` does not allow Aginctus to reinterpret an unmanaged network or a different network type.

## Herdr client image

Build the NixOS Incus image with:

```sh
just build herdr-client
```

This builds the `.#herdr-client` flake target. The result contains the Incus
metadata tarball and squashfs root filesystem for the current host architecture.

Update the pinned Herdr flake input when desired with:

```sh
just update nix herdr
```

Commit the resulting `flake.lock` change together with the image changes. Then import or replace the local development image alias with:

```sh
just update image herdr-client
```

The update target imports the image as `aginctus-herdr-client`, matching the
default `infrastructure.herdr.image.alias` configuration.

The image is based on NixOS 26.05. It consumes Herdr from the published Herdr flake and the repository `flake.lock` pins the exact Herdr source revision and its transitive Nix inputs. A systemd service starts `herdr server` in headless mode on boot. Herdr is therefore part of the declarative image rather than installed by Aginctus after container creation.

After updating the local image, exercise the lifecycle with:

```sh
go run ./cmd/aginctus herdr client ensure --dry-run
go run ./cmd/aginctus herdr client ensure
go run ./cmd/aginctus herdr client teardown --dry-run
go run ./cmd/aginctus herdr client teardown
```

The Herdr client instance is an Aginctus-owned container attached to the
configured management network. The guest-side SSH identity and authorization helpers are included in the images;
host-side remote registration, identity rotation and gateway credentials remain
separate integration work.

Both lifecycle operations accept `--dry-run` and `--force`. Teardown stops a
running owned container before deleting it. With `--force`, the stop request is
also forced and a same-named container may be adopted or removed despite missing
Aginctus ownership metadata, but a same-named VM is still rejected.

Common development commands are available through `just`:

```sh
just build
just test
just vet
just check
just fmt
```

`just fmt` formats both Go and Nix sources.

## Validation tiers

For routine development and required CI validation, run:

```sh
just check
nix flake check
```

`just check` verifies Go modules and runs Go tests and vetting. `nix flake check`
builds the lightweight NixOS module, gateway config parity, and workload socket
alignment checks, but intentionally **does not execute**
the firewall integration VMs. Those tests remain available as individually
buildable flake packages on `x86_64-linux`.

For the complete NixOS firewall VM suite, run:

```sh
just test-nix-firewall
```

To run only the relevant firewall backend test:

```sh
nix build -L --no-link .#firewall-iptables-integration
nix build -L --no-link .#firewall-firewalld-integration
nix build -L --no-link .#firewall-nixos-nft-integration
```

The VM tests exercise actual firewall enforcement, ownership revocation,
reconciliation, service restarts, and reboot behavior. Run them on an
`x86_64-linux` builder capable of executing NixOS virtual machine tests (with
hardware virtualization available where needed). They should be run before
merging firewall/security-boundary changes and during release validation, even
though they are not part of mandatory per-commit checks. A separately scheduled
or manually triggered CI job can also run these builds without making them
required checks; failures should remain visible.

`just test-nix` remains the separate heavyweight Incus/QEMU acceptance harness.

### Gateway configuration parity

`TestNixOSGatewayConfigParity` loads system configuration documents through the
production Go loader and `gateway.FromConfig`, then checks the generated Jsonnet
network resources and semantic equality of the human Compose renderer and
deployment Compose model. Cases cover module defaults, disabled egress, a
restricted DHCP pool with static addressing, and disabled DHCP with static
addressing. Assertions include management isolation, egress NAT/routing/IPv6,
literal DHCP keys, ownership guards, PostgreSQL's management-only attachment,
and LiteLLM's management gateway suppression and external egress CIDR metadata.

Plain Go tests use `internal/gateway/testdata/nixos-gateway.json`. The
`gateway-config-parity` flake check evaluates the NixOS module and builds its
actual `environment.etc."aginctus/config.json".source` for each case. It verifies
that the snapshot is semantically equal and runs the same Go test against those
generated documents. The fixtures are examples of the existing configuration,
not a second schema or a consolidation of Go/NixOS/guest defaults (#296).

These commands need Nix with flakes and the locked development dependencies,
but no Incus daemon, credentials, or running guests:

```sh
nix --quiet develop --command env -u AGINCTUS_INCUS_INTEGRATION -u AGINCTUS_INCUS_DISPOSABLE go test ./internal/config ./internal/gateway ./internal/orchestration ./internal/cli -count=1
nix --quiet develop --command env -u AGINCTUS_INCUS_INTEGRATION -u AGINCTUS_INCUS_DISPOSABLE bash -c 'go test ./... -count=1 && go vet ./...'
nix --quiet flake check
git diff --check
```

When intentionally changing module settings, regenerate the snapshot from the
module sources, review the resulting diff, and rerun the checks:

```sh
fixture=$(nix --quiet build --no-link --print-out-paths .#checks.x86_64-linux.gateway-config-parity.fixtures)
cp "$fixture" internal/gateway/testdata/nixos-gateway.json
```

Use `aarch64-linux` instead on an aarch64 builder. The check is render-level
proof only: actual OCI NIC addressing, default routes, TLS, and inference
acceptance remain separate live gates under #257 and #122.

## Continuous integration

`.github/workflows/hermetic.yml` enforces the supported hermetic validation
tier on `pull_request` to and `push` to `main`, with `workflow_dispatch` for
ad-hoc reruns. The job runs on `ubuntu-latest` with `permissions: contents: read`,
pins actions to specific commits, and uses `cachix/install-nix-action` v31.
It does not require or access Incus, secrets, OAuth credentials, or any
third-party tokens; the only network egress is fetching locked Nix inputs,
Go module cache, and the Nix binary cache.

The aggregate runs, in order, `nix --quiet develop -c just check-all` (added
by #347 / PR #356), `nix --quiet build --no-update-lock-file
.#checks.x86_64-linux.gateway-config-parity`, and `git diff --check` against
the previous commit (pushes) or the PR base ref (pull requests). Lock-file
drift is rejected by `--no-update-lock-file`. The Go module cache is restored
from `actions/cache` keyed on `go.sum` and populated online before the
offline aggregate, because `just check-all` runs with `GOPROXY=off`.

The job uses full-history checkout (`fetch-depth: 0`) so the diff step and
the recorded `BUILD_HASH=${{ github.sha }}` reflect the exact commit under
test. Skipped test counts are reported as expected skips in
`$GITHUB_STEP_SUMMARY` rather than as passes.

### Local reproduction

Reproduce the CI aggregate locally on a Linux host with flakes-enabled Nix:

```sh
# Optional: populate the Go module cache ahead of the offline aggregate.
nix --quiet develop -c go mod download

# Run the same aggregate the workflow runs.
nix --quiet develop -c just check-all

# Render-level NixOS gateway parity.
nix --quiet build --no-update-lock-file .#checks.x86_64-linux.gateway-config-parity

# Same whitespace check the workflow performs.
git diff --check origin/main...HEAD   # for a topic branch
git diff --check HEAD~ HEAD           # for the most recent commit
```

The aggregate fails rather than falling back to privileged execution if
unprivileged user namespaces are unavailable. On hosts without `/etc/subuid`
entries the three guest SSH host-trust tests
(`test_rejects_non_root_owned_trust_directory`,
`test_rejects_non_root_owned_state_root`,
`test_rejects_non_root_owned_known_hosts`) report
`skipped 'namespace UID mapping unavailable on this host'` and are tallied
as unittest skips. The CI summary explicitly lists those expected skips so
they are not mistaken for failures.

## direnv

If you use direnv with nix-direnv support, the repository includes an `.envrc`:

```sh
direnv allow
```

The generated `.direnv/` directory is ignored by Git.

## Incus development

The `incus` command in the development shell is only the client. It expects to connect to a usable Incus daemon.

Development code should not assume the daemon's storage pool, network names, or other host-specific defaults. Tests and future bootstrap commands should create or discover the resources they need explicitly.

Do not point destructive development commands at Incus resources that are not clearly owned by Aginctus.
