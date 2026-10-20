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

Creating firewalld zones and policies with `firewall-cmd` requires permanent
objects. Activating those objects with a reload replaces the runtime rule set;
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

The checker verifies the exact expected zone ownership description, interface,
host DNS/DHCP services, directional policy, priority, and source-limited HTTPS
rules, rejecting unknown additions and incomplete configurations. It does not
authorize activation. To remove **unactivated** staging after successful
verification:

```sh
aginctus gateway egress firewall unstage --uplink-zone=nixos-fw-default --confirm-permanent-only
```

The cleanup checks exact ownership before deleting the policy and zone, and
does not reload the runtime firewall. If these objects have already been
activated, removing permanent objects alone will not immediately remove the
runtime rules. Review the read-only audit first. The cleanup is not atomic;
partial failures are reported for manual recovery.

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
**Passing is necessary but not sufficient:** it does not inspect all daemon
settings, direct rules, the installed NixOS firewall module or nftables rules
managed by other daemons. It never authorizes or executes a global reload.

Firewalld's supported CLI only creates new named zones and policies in
permanent configuration. A controlled reload may eventually be needed to
activate these objects; do not replace that with an unrelated runtime rich
rule that accidentally opens a broader path.
