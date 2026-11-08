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

The opt-in `vmTests.x86_64-linux.home-manager-operator` output embeds the
standalone Home Manager CLI and generation closures in the disposable
NixOS/Incus VM. It creates a local consuming flake using only store-path
inputs. That flake exposes the exact generation closure evaluated by
the outer test derivation; the guest invokes
`home-manager --flake <path>#operator build --no-out-link` and
`home-manager --flake <path>#operator switch` as the **unprivileged user** with
no Incus authorization. This exercises the real CLI build/switch and activation
paths without claiming a from-scratch module evaluation or closure build in the
guest. The guest's NixOS activation does not run Home Manager. The VM remains
offline, and all inputs and outputs are already in its store. The test then
exercises repeat activation and a configuration-generation upgrade through the
CLI. An unmanaged configuration file blocks a switch without overwriting the
file or changing the active-generation GC root; the CLI generation profile
advances before activation, so a failed switch is not an atomic rollback.
Removing that conflict permits retry.
The test reactivates the recorded previous generation to roll back the user
configuration, checks the selected profile and CLI, then switches forward again.
This is package/configuration rollback, not Incus or credential rollback. It
checks the installed CLI/config and
OS-observed package identity, rejects mismatched system/user package declarations
and denied Incus access, and verifies unchanged Incus inventories on a disposable
Btrfs-backed pool and Linux bridge. Starting/stopping a lingering user manager
introduces no Aginctus user services or timers. The host independently runs only
root-managed DNS/DHCP and egress firewall services/timers; synthetic protected
host state stays unreadable/unwritable to the operator and unchanged. User
configuration requesting a trusted firewall zone does not alter host policy.

The disposable administrator separately provisions two runtime TLS identities:

- The restricted operator uses a project with `restricted=true` and
  `features.networks=true`, and can inspect the selected `aginctus` project and its
  profile fixture. Preflight still selects that exact project when ambient
  `INCUS_PROJECT` names `foreign`. Foreign-project access and default-project
  inspection are refused, as are default-profile writes, host bridge creation,
  project creation and server configuration changes.
- A separate broad administrator can inspect `aginctus` and `foreign` and
  create/delete a fixture bridge as a positive control for the denied operation.
  Preflight reports the unrestricted credential declaration separately from
  effective capabilities and still leaves write permissions unverified.

**Restriction limit:** Incus TLS authorization permits read-only access to
certain default-project resources for inheritance. The test explicitly observes
a readable default-project profile while its modification is denied. A successful
empty instance list is also not proof of project access: enumeration can filter
unauthorized objects. Strict default-resource metadata isolation is therefore
**not established by restricted TLS credentials**. Do not treat this fixture as
approval for a production authorization handoff.

Project network configuration is also part of the boundary: a restricted TLS
certificate alone is insufficient. With `features.networks=false` and no network
access allowlist, bridge creation through the selected project can reach shared
default-project networking. That configuration is not an accepted host/operator
split. The acceptance fixture
uses project-owned networks (`features.networks=true`), where Linux bridge
creation is unavailable. Shared-network authorization needs separate review
before any operator deployment.

Denied writes and the administrator's positive control exist only in this VM
fixture. Production preflight remains read-only. Reports are checked against
runtime certificate/key contents before returning them to the test driver.
Client configuration hashes stay unchanged across probes and reactivation;
no ambient `incus project switch` occurs. DNS positive and host-port negative
packet checks survive firewalld reload independently of the user manager.
Egress is dormant/fail-closed here; active gateway grant acceptance remains in
the separate firewall integration fixtures.

This exercises real guest CLI build and switch against the included local
configuration. It does **not** establish image/workload write authorization,
active-controller exclusion, credential migration/rollback or production
cutover acceptance.

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

The `nixos-module` evaluation check compares this mode with enabled system
lifecycle units for iptables-nft, NixOS-native nftables and firewalld (including
opt-in egress). Only the image/reconcile services disappear: host firewall
services and timers, effective INPUT integration, configuration, packages and
root-only firewall state rules remain identical. The Home Manager evaluation
check also verifies that user firewall settings emit only user package/config
declarations, including when the user requests a permissive zone.

The existing `firewall-iptables-integration`, `firewall-nixos-nft-integration`,
`firewall-firewalld-integration` and `firewall-egress-bootstrap` disposable VM
outputs exercise host-only packet policy, revocation and reload recovery.
The iptables scenario uses a synthetic bridge/rule fixture because the NixOS
Incus module requires nftables; it does not establish real Incus acceptance
on iptables. Gateway egress enforcement requires firewalld and remains opt-in.

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
