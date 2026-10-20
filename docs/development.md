# Development

Aginctus uses Nix to provide a small, reproducible development environment.

## Prerequisites

You need:

- Nix with flakes enabled;
- a Linux host; hermetic validation needs no Incus daemon;
- a host capable of running Incus, with an initialized daemon, only when
  exercising Incus-backed functionality.

The development shell supplies the Incus client and repository tooling. It does not install or manage the host's Incus daemon.

## Enter the development shell

Run:

```sh
nix --quiet develop
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

For the supported hermetic validation tier, run from the repository root:

```sh
nix --quiet develop -c just check-all
```

Inside the development shell, the same entrypoint is `just check-all`. It stops
at the first failing command and composes these individually runnable recipes:

| Recipe | Coverage |
| --- | --- |
| `check` | `go mod verify`, `go test ./...`, `go vet ./...` |
| `test-python` | Verbose `unittest` discovery of `test_*.py`, separately in `guest/opencode`, `guest/ssh`, `scripts/acceptance`, `scripts/gateway`, and `tests` |
| `test-shell` | `opencode-test.sh` and `opencode-live-adapter-test.sh`, using temporary fake adapters/hooks only |
| `check-shell` | `bash -n` for every Git-tracked `*.sh` file, including the opt-in operator harnesses without executing them |
| `check-nix` | `nix --quiet flake check --no-build --no-update-lock-file`, then `nix --quiet flake check --no-update-lock-file` |

The Nix checks evaluate the flake and build the current system's lightweight
NixOS module, gateway configuration parity, and workload socket alignment
checks. They do not build guest images or run firewall/Incus VMs. Other
architectures are not built by this command. This is a local validation
entrypoint; it does not configure or establish CI gates.

`check-all` overrides inherited Incus integration and live shell-acceptance opt-ins
before dispatch. It clears `GOFLAGS` so inherited flags cannot suppress Go tests.
It disables Go module/checksum downloads and automatic toolchain
selection (`GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`), so populate the Go module
cache first with `go mod download` in the development shell if needed. Nix may
download locked inputs and build dependencies; test traffic is limited to
loopback fakes. No real credentials, authentication, provider inference, privileged
host commands, or running Incus are required. Tests write temporary fixtures and
ordinary language/Nix caches, not host service configuration.

The SSH host-trust suite requires Linux unprivileged user/mount namespaces and
host `unshare`, `mount`, and `ssh-keygen` tools (util-linux and OpenSSH). It fails
if namespaces are blocked; it never falls back to host-root execution. Its three
foreign-ownership cases explicitly report skips when namespace UID mapping is
unavailable. Stage new shell files before checking so Git-based syntax discovery
includes them. New Python unit suites belong in the listed directories; audit
their external calls before adding them to routine discovery.

`tests/test_validation.py` runs the actual aggregate recipe in a temporary copy
with inert Go/Python/Nix commands and fake shell fixtures. It checks exact suite
dispatch, overrides hostile inherited opt-ins, and injects failures into every
child command plus Bash syntax validation. To run that proof alone:

```sh
nix --quiet develop -c python3 -m unittest discover -s tests -p 'test_*.py' -v
```

### Separate static-analysis work

Bash syntax checking is mandatory. Whole-tree `staticcheck`, `shellcheck`, and
`ruff` gates remain separate pending [#297](https://github.com/blevinn/aginctus/issues/297),
[#298](https://github.com/blevinn/aginctus/issues/298), and
[#299](https://github.com/blevinn/aginctus/issues/299). `check-all` neither silently
skips missing linters nor introduces competing configurations. For optional
analysis, install staticcheck/ruff separately (shellcheck is in the dev shell):

```sh
staticcheck ./...
git ls-files -z -- '*.sh' | xargs -0 -r shellcheck
ruff check guest scripts tests
```

These may report the outstanding findings tracked by those issues.

### Explicit opt-in validation

The standalone `guest/opencode/test_real_runtime.py` is not a `unittest` suite:
discovery imports it but does not execute its `main`. It needs a separately built
pinned OpenCode binary, and is excluded from `check-all`. To exercise the real
runtime against its loopback Responses mock, with synthetic credentials only:

```sh
nix --quiet build --no-link .#opencode-runtime
nix --quiet develop -c python3 guest/opencode/test_real_runtime.py "$(nix --quiet path-info .#opencode-runtime)/bin/opencode"
```

This extended runtime check is not evidence of live gateway or provider inference.

For the complete NixOS firewall VM suite, run:

```sh
just test-nix-firewall
```

To run only the relevant firewall backend test:

```sh
nix --quiet build -L --no-link .#firewall-iptables-integration
nix --quiet build -L --no-link .#firewall-firewalld-integration
nix --quiet build -L --no-link .#firewall-nixos-nft-integration
nix --quiet build -L --no-link .#herdr-socket-integration
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

`just integration-test --unsafe` runs the opt-in Go integration cases against a
real **disposable** Incus host, creating and deleting projects, containers and
volumes. Never run it against a production daemon. `--retain` preserves its test
projects for inspection. These tests are excluded even if the caller has exported
the integration opt-in variables before running `check-all`.

Live pilot, authentication, key/model registration, and inference helpers are
operator commands, not hermetic suites. Their prerequisites and exact invocations
are in [OpenCode acceptance](acceptance-opencode.md),
[live adapters](acceptance-opencode-live.md),
[Herdr acceptance](acceptance-opencode-herdr.md),
[workload-key pilot](gateway-workload-key-pilot.md), and
[inference smoke](gateway-inference-smoke.md). Review those runbooks and obtain
explicit authorization for the disposable topology, credentials and possible
billable inference before running them. No image import/update, CLI `doctor`,
firewall reconciliation, guest provisioning or live preflight is part of
`check-all`; only their isolated regression tests run.

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

`.github/workflows/hermetic.yml` runs the supported hermetic validation tier
on `pull_request` to and `push` to `main`, with `workflow_dispatch` for
ad-hoc reruns. It requires #347 / PR #356's `check-all` recipe. The job uses
`ubuntu-22.04`, `permissions: contents: read`, SHA-pinned actions, and
`cachix/install-nix-action`. Weekly Dependabot PRs propose action updates.
Checkout does not persist credentials. No production secrets or third-party
tokens are configured. Fetching source, actions, locked Nix dependencies and
Go modules requires network access; this workflow does not enforce an egress
allowlist. SHA pins identify action code, not a guarantee about all
network-fetched installer dependencies or hosted-runner software.

The aggregate runs, in order, `nix --quiet develop -c just check-all` (added
by #347 / PR #356), `nix --quiet build --no-update-lock-file
.#checks.x86_64-linux.gateway-config-parity`, and `git diff --check` against
the pre-push revision (pushes) or the PR base SHA (pull requests). Lock-file
drift is rejected by `--no-update-lock-file`. The Go module cache is restored
from `actions/cache` keyed on `go.sum` and populated online before the
offline aggregate, because `just check-all` runs with `GOPROXY=off`.

The job uses full-history checkout (`fetch-depth: 0`) so the diff step and
the recorded `BUILD_HASH=${{ github.sha }}` reflect the exact commit under
test. `$GITHUB_STEP_SUMMARY` reports step outcomes and observed Python test
results, including actual skip reasons. If the aggregate fails, its results
are partial: later suites have not run. Go and shell results remain in the
aggregate log. Gateway parity also runs after an aggregate failure when the
aggregate was attempted. Configured checks do not establish branch protection.

### Local reproduction

Reproduce the CI aggregate locally on a Linux host with flakes-enabled Nix:

```sh
# Optional: populate the Go module cache ahead of the offline aggregate.
nix --quiet develop --no-update-lock-file -c go mod download

# Run the same aggregate the workflow runs.
nix --quiet develop --no-update-lock-file -c just check-all

# Render-level NixOS gateway parity.
nix --quiet build --no-update-lock-file .#checks.x86_64-linux.gateway-config-parity

# Same whitespace check the workflow performs.
git diff --check origin/main...HEAD   # for a topic branch
git diff --check HEAD~ HEAD           # for the most recent commit
```

The runner contract requires unprivileged user namespaces with root UID
mapping, mount namespaces, and namespace-local bind mounts. A probe verifies
these before installing Nix, including that the bind mount is invisible outside
the namespace. If it fails, CI fails closed. Do not change host sysctls,
AppArmor policy, or run the SSH suite with sudo to bypass the contract.
An error such as `unshare: write failed /proc/self/uid_map: Operation not
permitted` is a blocking base-isolation platform failure, not an optional skip.
Foreign-ownership tests can still report `namespace UID mapping unavailable on
this host` independently of the mandatory base namespace contract; the summary
reports observed skips without predicting their count or cause.

The aggregate excludes live Incus/provider acceptance, real OpenCode runtime
execution, guest image builds, and heavyweight firewall/Incus/Herdr VMs. See the
validation tiers above for their separate commands. Whole-tree linters and link
checker integration are tracked separately in #297–#299 and #361.

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
