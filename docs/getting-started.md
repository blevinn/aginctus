# Getting started on a technical host

Aginctus controls Incus resources for a private Herdr/OpenCode environment and a LiteLLM inference gateway. This guide covers **installation, inspection and explicitly selected deployment**; it does not claim a successful live pilot. Read [security limitations](security.md) and check [v0.1 release acceptance](https://github.com/blevinn/aginctus/issues/118) before using real credentials or billable models.

For the pinned release-candidate deployment, credential lifecycle, optional real
coding smoke and independently checked cleanup, use the
[v0.1 operator handoff](v0.1-deployment-acceptance.md). Its full fresh-host
execution remains pending; existing pilot functional acceptance is separate.

## Host prerequisites

- Linux host with Nix flakes enabled; a working local Incus daemon and initialized storage pool are required only for Incus operations.
- For host-level NixOS integration, import `nixosModules.default` and enable `services.aginctus` using the [NixOS module example](configuration.md#nixos-module). A separate [Home Manager operator-only installation](home-manager.md) is proposed in the unmerged v0.2 stack; it cannot replace host firewall authority. The current guest images are NixOS containers; selecting `vm` requires an independently compatible image.
- Trust the host and Incus administrators; review the effective configuration, storage pools, bridge topology, firewall backend and network exposure before enabling mutations.

From the repository checkout, the development/build path is:

```sh
nix develop
just check-all
nix build .#aginctus
./result/bin/aginctus help
```

`just check-all` exercises hermetic checks, not real Incus provisioning or inference. On a NixOS deployment the CLI can instead come from `packages.<system>.aginctus` through the host module.

## Understand the NixOS activation boundary

Enabling the NixOS module is **not entirely observational**. Its `services.aginctus.firewall.enable` setting defaults to **true**: a boot-time service and recurring timer can reconcile host INPUT rules for Incus DNS/DHCP. Review [firewall integration](firewall-host-backends.md) and the [configuration contract](configuration.md#host-firewall-reconciliation-default-on) before activating the module; disable the firewall integration explicitly if it has not been approved.

Separately, `services.aginctus.reconcile.enable` defaults to **false**. Turning it on reconciles selected Incus resources; `services.aginctus.reconcile.gateway = true` additionally opts in to gateway apply. Successful reconciliation is not application readiness. Gateway runtime secrets must not be placed in Nix settings, the Nix store, command-line arguments or shared logs.

## Inspect first (no provisioning)

The commands below assume an installed `aginctus` on a configured host. They are read-only:

```sh
aginctus help
aginctus config get incus.management.network.name
aginctus doctor
aginctus network status
aginctus herdr client status
aginctus gateway validate
aginctus gateway render
```

`doctor` may report an absent Incus loopback HTTPS listener without altering it; `doctor --fix` would mutate that setting. `config show` is another read-only inspection command, but inspect its output before sharing any configuration. Gateway `validate` and `render` do not confirm routing, authorization, TLS or upstream inference.

For initial planning, preview the components that support non-mutating dry-runs:

```sh
aginctus network apply --dry-run
aginctus herdr client apply --dry-run
aginctus workload apply dev --dry-run
```

The `dev` example assumes a `workloads.dev` definition matching the [module configuration example](configuration.md#nixos-module). A successful preview is not an authorization to operate on production state.

## Explicit deployment after host approval

Choose an Aginctus-owned storage pool, imported guest images and configured workload before proceeding. In a checkout with the Nix development tools available, the image preparation path is:

```sh
just build herdr-client
just update image herdr-client
just build opencode-workload
just update image opencode-workload
```

**These image-update commands change local Incus aliases.** On NixOS, use the module-managed image import services when appropriate instead of independently managing the same aliases. Updating an image alias does not replace an existing running instance; preserve persistent SSH identity and state across approved upgrades.

After reviewing dry-run output, these operations **mutate Incus state** and must run only on an approved, appropriately configured host:

```sh
aginctus network apply
aginctus herdr client apply
aginctus workload apply dev
```

Then inspect `aginctus status`, `aginctus herdr client status` and `aginctus workload status dev`. If Herdr remote control is needed, consult [Herdr bootstrap and verification](herdr-machine-registration.md) to authorize, pin host trust and register the owned target before launching the interactive `aginctus herdr` console.

**Do not enable the gateway or run a live model request merely to finish this quickstart.** Gateway deployment, scoped key issuance, guest provisioning and egress firewall activation each have separate prerequisites, operator approvals and acceptance gates. Continue with [gateway documentation](ai-gateway.md), [operator key workflow](gateway-workload-key-pilot.md) and [release acceptance](acceptance-opencode.md) only after those prerequisites are satisfied.

For mutation categories, special `--force` behavior and troubleshooting pointers, use the [CLI guide](cli.md). For a full development test matrix and optional disposable-Incus tests, see [development](development.md).
