# Home Manager operator integration (v0.2 stack)

Status: **CLI-only standalone Home Manager integration merged** (#448); disposable VM validation and authorized user lifecycle support remain incremental v0.2 work.
See [design](design/home-manager-operator.md), [#426](https://github.com/blevinn/aginctus/issues/426)
and [NixOS configuration](configuration.md#nixos-module). This does **not**
replace the v0.1 pilot activation/runbooks.

## Standalone Home Manager: CLI and user configuration only

An example consuming flake, installed using the operator's **standalone**
Home Manager (replace the username, home directory, and system):

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    home-manager = {
      url = "github:nix-community/home-manager/release-26.05";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    aginctus.url = "github:blevinn/aginctus";
  };

  outputs = { nixpkgs, home-manager, aginctus, ... }: {
    homeConfigurations.operator = home-manager.lib.homeManagerConfiguration {
      pkgs = nixpkgs.legacyPackages.x86_64-linux;
      modules = [
        aginctus.homeManagerModules.default
        {
          home.username = "operator";
          home.homeDirectory = "/home/operator";
          home.stateVersion = "26.05";
          programs.aginctus = {
            enable = true;
            settings.gateway.id = "local";
          };
        }
      ];
    };
  };
}
```

The opt-in `vmTests.x86_64-linux.home-manager-operator` output evaluates
and builds a standalone Home Manager generation in the outer Nix build, then
activates it **as an unprivileged user** inside a disposable NixOS/Incus VM
with no Incus authorization. Prebuilding is intentional: the VM is offline,
so it cannot fetch build-time sources during a nested `home-manager build`.
The test exercises the real Home Manager activation twice, checks the
installed CLI/config and verifies no Incus mutations, but it does **not**
execute the `home-manager build/switch` CLI inside the guest.

Run the heavyweight acceptance test explicitly:

```sh
nix build -L --no-link .#vmTests.x86_64-linux.home-manager-operator
```

It is deliberately **not exported under `packages` or `checks`**. Routine
`nix flake check` evaluates package derivations, while this test's evaluation
imports Home Manager from its packaged source (an import-from-derivation).
The dedicated output avoids introducing that evaluation-time build into
hermetic CI. Its runtime source and closure are still pinned through Nixpkgs.
This initial scenario does not establish permissions for subsequent
image/lifecycle work.

The consumer's flake lock pins the input revisions. The module exports
`homeManagerModules.aginctus` and `homeManagerModules.default`. It installs
the Aginctus CLI in `home.packages` and creates
`xdg.configFile."aginctus/config.json"` from the same typed defaults
used by NixOS. The CLI reads this file using its existing XDG rules.

The `operator-settings-parity` flake check compares complete rendered Home
Manager and NixOS settings through the Go loader using shared fixtures. It
covers defaults, explicit settings, freeform forward-compatible keys and
environment/explicit-file/CLI precedence. JSON dotted keys remain literal keys;
CLI `--config path=value` uses dotted paths. Runtime secret keys are rejected
in nested, dotted and case-varied forms, including `null` values.

Shared `firewall` settings describe CLI intent, not host authorization. Home
Manager exposes no NixOS `services.aginctus.firewall` policy switches and does
not install privileged services. User configuration cannot grant firewall or
bridge authority; privileged host policy remains separately administered.

`home-manager build --flake .#operator` evaluates the user generation without
activating it. After inspecting the closure and reviewing any conflicts with
existing `~/.config/aginctus/config.json`, a standalone operator can use
`home-manager switch --flake .#operator`. **Do not test activation on the pilot
host without authorization.**

This installation does not configure or contact Incus, create/reconcile bridges,
import images, run privileged diagnostics, configure systemd user services,
or touch gateway credentials. It does **not** grant administrative authority.
The user still requires separately provisioned Incus authorization to operate
resources. `incus-admin` access is effectively host-root-equivalent.
Restricted project identities cannot provision host-level Linux bridges.

## NixOS: retain host firewall authority without guest orchestration

An existing, approved NixOS host can keep just the NixOS-side Aginctus policy
and omit automatic guest image alias updates and reconciliation:

```nix
{
  services.aginctus = {
    enable = true;
    firewall.enable = true;

    images.herdrClient.enable = false;
    images.opencodeWorkload.enable = false;
    reconcile.enable = false;
  };
}
```

**Preserve any previously approved gateway egress/firewalld options** and
other host-specific security configuration separately. The snippet above is
not a full host firewall configuration and does not authorize applying it.
The existing NixOS module still creates and periodically reconciles the
ownership-scoped DNS/DHCP host firewall policy. If egress enforcement is
enabled on the host, it stays root-managed; it must not migrate to Home Manager.

These system-option combinations already exist; the stack adds regression
assertions to ensure disabling image and workload reconciliation does not
also disable the host firewall integration.

## What has not migrated

- There are **no Home Manager image-update or workload-reconcile user units**
  in the initial CLI-only slice. Continue using the supported system workflow
  or explicit, authorized CLI invocations; do not attempt two controllers.
- Gateway master state already owned by the system remains under
  `/var/lib/aginctus/state`; operator state under the user's XDG directory
  is a **different credential owner**. Never silently copy or chown either.
- Incus project-scoped operator access and bridge permission handoff depend
  on [#198](https://github.com/blevinn/aginctus/issues/198) and
  [#200](https://github.com/blevinn/aginctus/issues/200).
- Safe owner-conflict detection and a reversible cutover are tracked in
  [#441](https://github.com/blevinn/aginctus/issues/441) and
  [#442](https://github.com/blevinn/aginctus/issues/442).
- Live operator deployment and firewall policy should be validated inside
  a disposable NixOS/Incus test VM before any pilot migration (#443).

Use `aginctus version` and `aginctus config show` for non-mutating local
inspection, and keep runtime gateway secrets out of Nix settings, activation
logs, command-line options and configuration files.
