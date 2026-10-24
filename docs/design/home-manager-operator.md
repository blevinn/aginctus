# Home Manager operator / NixOS host authority

Status: **accepted design target for v0.2; implementation is incremental**.
Tracking: [#426](https://github.com/blevinn/aginctus/issues/426),
[#434](https://github.com/blevinn/aginctus/issues/434).
This is not a v0.1 host migration or authorization to modify a pilot.

## Decision

Use **standalone Home Manager** as the preferred *operator* integration, alongside
the existing NixOS module as a *host capability provider*.

| Capability | Owner | Boundary |
| --- | --- | --- |
| CLI package, JSON settings | `programs.aginctus` (Home Manager) | User profile and XDG configuration |
| Explicit image import and selected reconciliation | `services.aginctus` (Home Manager; future, opt-in) | Authorized Incus API calls; no implicit escalation |
| Incus daemon, storage pools, local authorization | Host administrator / NixOS | Privileged host resources |
| Managed Linux bridges | Host-level Incus network ownership | Not available solely through restricted project access |
| DNS/DHCP INPUT and gateway egress firewall | NixOS `services.aginctus` | Root-managed effective firewall chains and firewalld objects |
| Root-owned gateway secrets/firewall journal | Existing system authority | Must not be implicitly copied into operator XDG state |

Home Manager's `programs.aginctus.enable` installs the CLI and materializes
**nonsecret** configuration under `~/.config/aginctus/config.json` (respecting
Home Manager's XDG configuration handling). Activation must not create or modify
Incus resources, restart the gateway, register agents, or install user services.

The proposed `services.aginctus` operator namespace is separately disabled by
default. When implemented, image updates and selected reconciliation must
require explicit operator invocation/opt-in and capability preflight. A normal
`home-manager switch`, package upgrade, or login must not deploy workloads
or update running image aliases.

Keep the existing `services.aginctus` NixOS option/API for compatibility.
Its host firewall and egress policy remain mandatory where the deployment
requires them. Extracting privileged services is a separate reviewed change,
not a reason to relax #220 or #363.

## Authorization model

Home Manager never edits group membership, authorizes a TLS client, or invokes
`sudo`. Incus `incus-admin` access is effectively host-root authority; the
restricted `incus` group/project model is not a substitute for host-managed
Linux bridge creation. A denied or unavailable capability is a normal,
actionable preflight failure. Never switch the user's ambient Incus project:
pass the selected project explicitly as #198/#200 are implemented.

The privileged firewall layer validates real Incus ownership and exact
bridge/interface identities, rather than trusting a user-controlled
configuration file or a resource name prefix. It must not accept arbitrary
user-provided firewall commands, install a second permissive nftables base
chain, or trust entire Incus interfaces.

## Configuration and secrets

The existing JSON precedence (CLI override, explicit file, environment, user
file, system file, built-ins) stays intact. Share the Nix typed settings
registry between Home Manager and NixOS and test it against Go defaults.
Reject runtime-only keys `gateway.postgres.password`, `gateway.master.key`
and `gateway.salt.key` before they can enter a world-readable Nix store
closure, including when a nested object supplies a `null` value.

Do not conflate `/var/lib/aginctus/state` (root-owned NixOS reconciliation)
with `~/.local/state/aginctus` (operator-owned). A prior pilot mismatch caused
a 401 (#122). A state handoff is a distinct, authorized, backed-up transaction
with exactly one active owner; never auto-`chown`, copy, rotate, or initialize
secrets when switching Home Manager generations.

## User service semantics

System and user service managers do not share ordinary `After=` ordering.
An optional user unit must independently check Incus reachability,
authorization and selected project; bound waits and failures. Account for
login-only managers versus lingering, restart behavior during Home Manager
activation, explicit destructive-operation authorization, and competing system
and user reconcilers. Default to no boot/login reconciliation.

## Alternatives rejected

- **Home Manager manages firewall**: cannot own host nftables/firewalld policy
  without dangerous, implicit privilege escalation.
- **Implicit `sudo`/`incus-admin` provisioning**: conceals the real host
  authority boundary and is unsafe for agent-controlled operators.
- **Single automatic `home-manager switch` deployment**: package/config
  updates must not change live workloads.
- **Automatic secret-state relocation**: risks duplicate credential owners,
  authorization failure and irrecoverable rotation.
- **Defer everything until native reconciliation**: #153 is independent of the
  non-mutating operator installation path.

## Incremental delivery / acceptance

1. Reuse a shared typed JSON settings schema without changing NixOS output
   or default behavior (#435).
2. Export a non-mutating standalone Home Manager module (#436), with
   evaluation/secret-rejection tests and operator-only documentation.
3. Add read-only Incus capability preflight (#437), then *optional* image
   updates and scoped reconciliation (#438/#439) after #198/#200 supplies an
   unambiguous project scope.
4. Preserve the authoritative NixOS host module (#440), guard against
   competing system/user owners and version skew (#441), and perform any
   reversible ownership cutover separately (#442).
5. Validate in disposable VMs (including Btrfs/bridge policy and forbidden
   operations), not on the pilot host (#443), and document the supported
   combined deployment (#444).

An HM-only installation can inspect/build without Incus authorization.
**No supported mode claims rootless Incus bridge or firewall administration.**
