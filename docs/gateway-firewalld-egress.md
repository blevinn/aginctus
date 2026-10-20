# LiteLLM egress and firewalld forwarding

The gateway's Incus-managed egress bridge provides NAT and IPv4 forwarding. On
hosts with firewalld, a later forward base chain can still drop packets even
when Incus permits them. This was confirmed on the pilot: the TCP SYN traversed
the egress bridge and Incus chains but was dropped by
`inet firewalld filter_FWD_nixos-fw-default_to_nixos-fw-default`.

Aginctus currently does **not** install host firewalld forwarding policy. The
read-only command

```sh
aginctus gateway egress firewall plan --uplink-zone=nixos-fw-default
```

emits a scoped proposal based on the effective configuration. The uplink zone
is an explicit operator input; it cannot be safely inferred from the gateway
bridge CIDR alone. The proposal describes a dedicated `aginctus-egress` zone,
the owned bridge interface, and a directional HTTPS policy to the specified
uplink zone. It includes rich-rule rejects for common private IPv4 ranges,
and deliberately keeps the policy target `CONTINUE` rather than trusting the
interface or enabling blanket forwarding. This is **advisory only**, not a
complete confinement policy.

## Activation boundary

Do not apply the output directly as commands. First verify the bridge's Incus
ownership, the uplink zone and interface, policy/zone name ownership, all
forwarded destinations that must be excluded (including local non-RFC1918
nets), any existing policy, and rollback requirements.

Creating named zones and policies through the supported CLI or D-Bus API
requires permanent objects. Runtime `zone.setZoneSettings2` and
`policy.setPolicySettings` update existing objects, not create them. The
installed daemon's setters first look up the named runtime object. The
`config.addZone2` and `config.addPolicy` methods create permanent configuration.
Activating those objects with a reload replaces the runtime rule set;
Aginctus must not reload firewalld as a side effect of `gateway apply`.
This also means an extra nftables `accept` in an earlier base chain cannot
replace a firewalld forwarding policy.

## Further integration work

A separate implementation is needed for lifecycle-aware, ownership-verified
zone/policy management, supported firewall backends, safe activation without
clobbering unrelated runtime-only rules, cleanup, and runtime verification.
It must fail closed if the effective policy cannot be established. Until then,
gateway egress cannot be considered production-ready on hosts with a rejecting
firewalld forwarding policy.

Revisit issue #257 acceptance only after HTTPS works from LiteLLM, management
isolation still holds, and an authenticated Responses call succeeds.

## Explicit permanent-only staging (experimental)

`aginctus gateway egress firewall stage --uplink-zone=nixos-fw-default --confirm-permanent-no-reload` creates a dedicated zone and directional policy **in firewalld permanent configuration only**. It does not call reload or change runtime forwarding. It refuses existing zone/policy names instead of taking ownership of them and attempts rollback of objects created during a failed staging attempt. It is one-shot staging, not idempotent reconciliation or production lifecycle management. Operators must review the exact permanent objects and diff before activation.

The dedicated zone uses the firewalld default target and explicitly permits DNS and DHCP to the host, preserving gateway name resolution and address allocation. It stages source-specific forwarded HTTPS acceptance and earlier private, loopback, link-local, multicast, and carrier-grade NAT destination drops, rather than adding a broad HTTPS service to the zone. It does not fully enforce public endpoint allowlisting. Existing Nix-managed host configuration can supersede firewalld permanent definitions, so do not activate on a NixOS system without reviewing the host firewall module.

**Do not activate the staged policy yet:** staging is a review boundary. Once the PR has been validated, the operator must separately decide on activation semantics, runtime-only rule preservation, rollback, and NixOS declarative ownership. The existing `gateway apply` intentionally remains unaffected.

## Read-only activation audit

`aginctus gateway egress firewall audit --uplink-zone=nixos-fw-default` compares names and details of permanent and runtime firewalld zone/policy objects and the bridge's current zone assignment. It never activates, reloads, or writes configuration; `activation_approved` remains false even when all objects are present. Inspect its output to identify whether permanent staging exists and whether a runtime reload or other actor has activated a policy. No individual object detail should be taken as proof that the complete host firewall policy is safe. An explicit, host-aware activation mechanism and negative isolation tests remain separate work.

## Ownership verification and rollback

After operator-approved permanent staging, run

```sh
aginctus gateway egress firewall verify-staged --uplink-zone=nixos-fw-default
```

The checker requests verbose object information to read ownership descriptions
and verifies the expected zone ownership description, interface,
host DNS/DHCP services, directional policy, priority, and source-limited HTTPS
rules. It rejects additional protocols, source ports, masquerading, forward
ports, zone rich rules, and incomplete configurations. Service order does not
affect verification. Unsupported output formats fail closed. It does not
authorize activation. To remove **unactivated** staging after successful
verification:

```sh
aginctus gateway egress firewall unstage --uplink-zone=nixos-fw-default --confirm-permanent-only
```

The cleanup checks ownership and refuses deletion if either named object is
loaded in runtime, even if currently inactive or only partly activated. It
deletes the permanent policy before the zone and never reloads. Operators must
serialize firewall maintenance: the read/check/delete sequence is not atomic
against another administrator or daemon. Partial failures are reported for
manual recovery; a policy-deletion failure leaves the zone untouched.

### Activation constraint

Do not blindly run `firewall-cmd --reload` on the NixOS pilot. Reload replaces
firewalld runtime-only configuration with its permanent configuration, and the
pilot has unrelated security-critical policies. An operator must separately
review those differences and choose a safe host-specific activation method,
then verify external HTTPS and negative management isolation. Aginctus does not
offer an automatic activation command until the entire runtime state can be
preserved or safely reconstructed. The existing stage and unstage commands are
not a substitute for that proof.

### Read-only global reload parity guard

`aginctus gateway egress firewall reload-parity` compares a selected subset
of permanent and runtime firewalld state, excluding the staged egress
zone/policy from the permanent side. It refuses mismatches and makes no changes.
Runtime header annotations such as `(active)` and `(default)` are ignored;
interface bindings, permissions, and other property lines remain significant.
**Passing is necessary but not sufficient:** it does not inspect all daemon
settings (including the default zone), direct rules, service/IP-set contents,
the installed NixOS firewall module or nftables rules managed by other daemons.
It never authorizes or executes a global reload.

Firewalld's supported CLI only creates new named zones and policies in
permanent configuration. A controlled reload may eventually be needed to
activate these objects; do not replace that with an unrelated runtime rich
rule that accidentally opens a broader path.

## NixOS lifecycle boundary

The host module predeclares the management DROP zone and forwarding-denial
policies. Aginctus's `network firewall reconcile` verifies Incus bridge ownership
and installs runtime-only IPv4 DNS rules to the bridge address plus UDP DHCP.
Its timer periodically repairs these grants. A firewalld reload removes the
grants until successful reconciliation; this is an availability interruption,
not permission to replace ownership checks with permanent unrestricted DNS.
NixOS supplies firewalld configuration through store-backed configuration paths
and reload triggers, so imperative staging is not declarative deployment.

There is no supported automatic egress activation or reconciliation yet.
A safe lifecycle implementation needs predeclared, default-deny egress objects,
an explicitly approved initial host activation, and ownership-verified runtime
grants that can be withdrawn and restored without subsequent global reloads.
It also needs independent bidirectional forwarding denials so unrelated trusted
zones cannot provide an alternative route, destination exclusions including
host-connected public networks, and actual packet-level regression tests.
The current staged CONTINUE policy and common-private-range exclusions do not
provide those guarantees. Do not activate them as a complete isolation policy.

LiteLLM intentionally retains management-network access to PostgreSQL. Routed
egress isolation cannot prohibit that same-L2 dependency. Acceptance must
distinguish the required database connection from prohibited routed access;
per-workload management isolation requires a separate network design.
