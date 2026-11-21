# Home Manager operator integration (v0.2 stack)

Status: **Standalone Home Manager CLI/configuration and opt-in environment candidate output are available.** Actual project-scoped accepted-state promotion, authorized user lifecycle and migration remain gated; disposable VM checks cover only the specific cases described below.
See [design](../../design/home-manager-operator.md), [#426](https://github.com/blevinn/aginctus/issues/426)
and [NixOS configuration](../../reference/configuration.md#nixos-module). This does **not**
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
checks the installed CLI/config and observed package identity, verifies
denied Incus access for an uncredentialed operator, and verifies unchanged
Incus inventories on a disposable Btrfs-backed pool and Linux bridge.
**It does not compare system and user package declarations**: the former
package-parity ownership audit was removed in [#923](https://github.com/blevinn/aginctus/pull/923).
A package version or installed unit is diagnostic context, not proof of an
authorized environment writer. Starting/stopping a lingering user manager
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
uses project-owned networks (`features.networks=true`) and an explicit
`restricted.networks.access=hm-mgmt` allowlist. The bridge remains in `default`:
the restricted operator's network API returns `Network not found` in both the
isolated and default projects even with that allowlist. A separate administrator
can inspect the bridge. Masked reads do not establish write denial, so a direct
PATCH to its actual default namespace must fail authorization. The VM also
requires refusal of project restriction changes, storage pool changes and
foreign-project volume creation, and checks unchanged volume inventories.
Creation of a non-allowlisted network in the selected project must be refused
by project policy before network-driver validation.
An administrator performs the same bridge PATCH and removes its test key as a
positive control. This narrows the fixture boundary; it does **not** prove NIC
attachment to that bridge or validate `features.networks=false` project
delegation or substitute for a server-authorized resource writer. The additional
policy line in `operator preflight` is read-only project metadata and explicitly
never upgrades the preflight exit status into write authority. The complete
controller-exclusivity and accepted-revision gates remain unimplemented.

Denied writes and the administrator's positive control exist only in this VM
fixture. Production preflight remains read-only. Reports are checked against
runtime certificate/key contents before returning them to the test driver.
Client configuration hashes stay unchanged across probes and reactivation;
no ambient `incus project switch` occurs. DNS positive and host-port negative
packet checks survive firewalld reload independently of the user manager.
Egress is dormant/fail-closed here; active gateway grant acceptance remains in
the separate firewall integration fixtures.

The VM quiesces the host reconcile timers before administratively stopping and
runtime-masking both the Incus service and socket, avoiding competing systemd
start/stop jobs while leaving filtering rules installed. Restricted
operator preflight fails while the daemon is unavailable; real Home Manager CLI
switches to both configurations still succeed without restarting it. After the
disposable administrator unmasks and starts Incus and restores the timers, the same restricted identity
passes preflight. Client credentials/configuration and resource inventories stay
unchanged, and DNS positive/host-port negative checks pass after host firewall
reconciliation and test-driver reattachment of its synthetic peer to the recreated
bridge. This tests clean daemon interruption, not workload recovery or a crash during an Incus
write transaction.

Gateway credential metadata acceptance uses separate runtime-generated synthetic
host and user files with owner-only permissions. A privileged host diagnostic
inspects only the host location; the ordinary operator inspects only its
user credential location. Neither comparison nor a selected controller owner
is inferred. User files with broad modes, symlinks to protected system state,
or foreign ownership are reported unsafe without automatic repair. Reports
are checked for synthetic secret disclosure; Home Manager switching preserves
both state trees' contents and owner/mode metadata. These are read-only
observations, not proof of a running gateway's credential identity or an
authorization/migration acceptance.

Lifecycle unit-inventory acceptance starts harmless sleep fixtures under a known
Aginctus lifecycle unit name in the real system/current-user managers. Installed,
inactive and active units are reported without being interpreted as competing
project authorities; even simultaneous system/user fixtures are observational.
Removing both fixtures restores an absent inventory, while stopping the user
manager yields unverified evidence and a nonzero result. Root firewall-only units
remain outside that allowlist.
This verifies read-only diagnostics, not mutation-time guards, real reconciler
behavior, other-user visibility, or exclusion of manual/renamed controllers.

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

## Explicit environment candidate (current direction; no activation)

The Home Manager option `programs.aginctus.environmentCandidate` (default
`null`) emits a complete, **nonsecret**, versioned
`EnvironmentCandidate` to
`$XDG_CONFIG_HOME/aginctus/environment-candidate.json`. It is distinct
from ordinary `programs.aginctus.settings` and the legacy
`workloadCandidate` below. For example, add to the existing
`programs.aginctus` declaration:

```nix
environmentCandidate = {
  id = "123e4567-e89b-42d3-a456-426614174000";
  name = "example-environment";
  serverId = "example-incus-server";
  project = "example-project";
  workloads = [ ];
};
```

The `id` is an immutable environment identity; `name` can be renamed.
The `serverId` and `project` are **declared bindings**, not verified
authority or trusted evidence of an Incus connection. One Incus project may
contain several independently managed environments. The candidate's
project may differ from ordinary `settings.incus.project`; the latter
does **not** override or redirect the environment's binding.

A caller must explicitly select one candidate file through the
[versioned source-neutral API](../../design/environment-candidate-sources.md)
and compare its identity with independently authenticated Incus server/project
observations before any future accepted-state workflow. NixOS, Home Manager
and local-file candidates are **alternatives**, never privileged partial
system/user fragments to merge. Rendering or switching a Home Manager
generation never contacts Incus, promotes an accepted revision, creates a
state service or starts a workload. A project-scoped accepted-state writer,
controller fence and activation remain independently gated by
[#695](https://github.com/blevinn/aginctus/issues/695),
[#696](https://github.com/blevinn/aginctus/issues/696) and
[#704](https://github.com/blevinn/aginctus/issues/704).

The Home Manager module rejects simultaneous declarations of
`environmentCandidate` and `workloadCandidate`. Keep all secrets,
OAuth tokens and host firewall configuration out of candidate JSON and Nix
derivations.

## Legacy experimental workload candidate (no activation)

A Home Manager operator can optionally render a **nonsecret, workload-only
configured candidate** that is compatible with the experimental
[local accepted-state workflow](../accepted-state-local.md).
This is not the configured-to-accepted promotion of a production deployment,
a runtime reconciler, or an Incus privilege grant.

The strictly typed option is `programs.aginctus.workloadCandidate` (default
`null`). For example, inside the same Home Manager module as
`programs.aginctus.settings`:

```nix
{
  programs.aginctus = {
    enable = true;
    settings.incus.project = "aginctus";

    workloadCandidate = {
      deploymentID = "example";
      workloads = [
        {
          id = "dev";
          instanceName = "example-dev";
          instanceType = "container";
          # Synthetic 64-character demonstration fingerprint ONLY.
          # Replace with an approved, immutable, exact image fingerprint
          # before using an actual accepted-state experiment.
          imageFingerprint = builtins.concatStringsSep "" (builtins.genList (_: "a") 64);
          profiles = [ "default" ];
          ensureRunning = false;
        }
      ];
    };
  };
}
```

Home Manager writes `~/.config/aginctus/workload-candidate.json` (normally
an XDG symlink into the Nix store) **only when the option is present**. The
candidate is exactly `WorkloadDeployment` v1alpha1: explicitly selected IDs,
immutable lowercase SHA256 image identities, ordered profile names, instance
types and running-state *intent*. The Incus project is taken from the user's
**same** `settings.incus.project` option; it cannot be selected separately
from a CLI ambient default. Duplicate workload IDs/instance names/profiles,
unbounded selections, image aliases and additional credential/host policy
fields are rejected. Do not place secrets, OAuth state or private keys in
this option: Nix outputs are not secret storage.

A normal `home-manager switch` only changes the candidate file. It does
**not** create a private accepted-state store, accept a revision, start an
Aginctus user service, or contact Incus. `ensureRunning` describes candidate
intent; it does not cause a start at login, activation, or package update.

To **inspect** that candidate explicitly in a scratch, same-UID local store,
install the separately opt-in `state-local` package and run:

```sh
nix shell .#state-local
state_dir="$(mktemp -d)"
chmod 700 "$state_dir"
aginctus-state-local init --state-dir "$state_dir/accepted"
aginctus-state-local diff \
  --state-dir "$state_dir/accepted" \
  --deployment example --project aginctus \
  --candidate "$HOME/.config/aginctus/workload-candidate.json"
```

The project and deployment arguments are **required** and must match the
candidate. Diff is read-only and reports changed field names, not workload
values; it cannot modify Incus. Any manual local acceptance requires
`aginctus-state-local accept` with an exact `--expected-revision` and
`--confirm`, and is **only** a same-user experimental store operation. It
does not grant controller ownership or authorize native reconciliation.
Never run that command as root on behalf of a Home Manager user or point it
at the pilot/system store.

The authoritative host bridge, storage pools, firewall and root gateway
credentials remain NixOS-owned. A confined Incus TLS credential can be
delegated only after the separate [authority review](../../design/home-manager-delegated-authority.md)
and [controller fence](../../design/controller-ownership.md) pass. The
disabled-by-default, explicitly triggered **mutating** image/workload services
remain tracked in [#438](https://github.com/blevinn/aginctus/issues/438)
and [#439](https://github.com/blevinn/aginctus/issues/439). This candidate
feature does not implement either one.

## Supported operation and permission boundaries

| Mode | Package/configuration activation | Host firewall, bridges and pools | Incus lifecycle |
| --- | --- | --- | --- |
| NixOS-only host | Privileged host activation, after administrator review | Host administrator / NixOS only | Existing explicit or separately enabled NixOS operations |
| Standalone Home Manager operator | Unprivileged `home-manager build/switch`, settings and candidate files only | **Not permitted by HM** | No automatic actions; separate Incus credentials and explicit authorization required |
| Combined NixOS host + Home Manager operator | Independent host and user generations | Host NixOS remains authoritative | Future selected environment operations require real project authorization and write-time fencing |

`aginctus version`, `aginctus config show`, `home-manager build` and
`aginctus operator preflight --project <project>` are non-mutating
inspections (preflight performs daemon reads only). A **NixOS activation**,
a future environment accept/apply, image import, project creation, or any
firewall reconciliation is a distinct mutating action requiring an
authorized operator and explicit review; none is implied by changing an HM
candidate or by `operator preflight` success. Do not run destructive
cutover or bridge/firewall mutation on the pilot host as part of a module
documentation check.

An HM-only installation never creates systemd user reconciler units.
System and user systemd managers have independent lifecycles:
`After=incus.service` in a user unit does not establish host service
ordering. Any future user unit must handle login, lingering, bounded daemon
availability and explicit Incus credentials without `sudo` or automatic
`incus-admin` enrollment.

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
  **Migration or replacement** of an existing deployment is a separate
  authorized, inventoried, backed-up operation tracked in
  [#202](https://github.com/blevinn/aginctus/issues/202); switching HM
  generations is **not** Incus migration or credential rollback.
- Safe owner-conflict detection and a reversible cutover are tracked in
  [#441](https://github.com/blevinn/aginctus/issues/441) and
  [#442](https://github.com/blevinn/aginctus/issues/442).
- Live operator deployment and firewall policy should be validated inside
  a disposable NixOS/Incus test VM before any pilot migration (#443).

Use `aginctus version` and `aginctus config show` for non-mutating local
inspection, and keep runtime gateway secrets out of Nix settings, activation
logs, command-line options and configuration files.
