# Management DHCP gateway policy

The shared management-network renderer declares `ipv4.dhcp.gateway=none` by
default. `network ensure` and gateway deployment use the same policy, including
custom bridge names and CIDRs. CLI configuration and NixOS settings expose it as
`incus.management.network.ipv4.dhcp.gateway`. An explicit alternative gateway
is an operator policy override and will not satisfy the strict no-default-route
pilot preflight. An empty value renders as `none`.

[Incus bridge documentation](https://linuxcontainers.org/incus/docs/main/reference/network_bridge/)
defines `none` as disabling gateway announcement, not DHCP itself. DHCP remains
enabled by Incus's default; addressing, on-link IPv4 routes, bridge DNS and
private peer connections remain available. NAT and routing remain disabled and
IPv6 remains `none` by default. The separate gateway egress bridge and LiteLLM's
egress default route are unaffected. This bridge option is distinct from the
OCI NIC `ipv4.gateway=none` extension required by incus-compose. PostgreSQL and
LiteLLM management attachments explicitly set `x-incus-compose.internal=true`,
including deployments without egress, to retain OCI on-link addressing without
depending on bridge gateway discovery. This requires the same OCI NIC extension
as LiteLLM's dual-NIC topology. The disposable
storage/network and gateway topology scenarios validate the bridge option with
their pinned Incus packages; an unsupported daemon must fail rather than silently
fall back to advertising a gateway.

## Existing pilot: read-only inventory first

Related corrective action: [#571](https://github.com/blevinn/aginctus/issues/571).
This is a maintenance plan, not permission to modify the live pilot. A normal
`network ensure`, gateway up, or system reconciliation rejects existing bridge
DHCP policy drift through an existing-config precondition; it does not migrate
the bridge. Mutable network settings are supported by Apply, so
`rejectUnsupportedChanges` alone does not prevent live DHCP changes. That option
remains enabled alongside the explicit DHCP policy precondition. Ownership-safe
deletion does not require the new DHCP setting.
Do not use `--force`, teardown/recreation, or ownership relabeling to
bypass that protection. Schedule migration before deploying the changed default
to a reconciled existing host, since reconciliation can otherwise fail closed.

1. Record the installed CLI revision, effective configuration sources and
   `aginctus config get incus.management.network.name`. Confirm the local Incus
   endpoint and actual projects with the operator. v0.1 management lifecycle
   targets the default project; the intended `incus.project` setting does not
   relocate those resources. Do not point this procedure at a remote daemon.
2. With a private operator-owned directory (`umask 077`), preserve the complete
   network response, profiles and each attached guest's local and expanded
   configuration, current state, route tables, resolver configuration and DHCP
   lease/client details. Inventory attachments across projects using the network
   `used_by` list; do not infer ownership from names. Expanded configuration can
   contain secrets: keep it local and out of GitHub/evidence uploads. Arrange
   backups for any stateful guest that might be restarted.
3. Verify the target is a managed bridge with both exact markers
   `user.aginctus.managed=true` and
   `user.aginctus.resource=management-network`. Check its address against the
   effective settings, `ipv4.nat=false`, `ipv4.routing=false`,
   `ipv6.address=none`, DHCP not disabled, and no unexpected `raw.dnsmasq` or
   DHCP static-route overrides. An unset gateway is the expected legacy case;
   stop for review if another explicit gateway is present.
4. Save the **presence and exact value** of `ipv4.dhcp.gateway`, including the
   distinction between unset and `none`. Review before/after plans and the
   specific guests that need route renewal. Pause competing reconcilers only
   within the approved maintenance window, recording their prior state. Preserve
   firewalld and the bridge's ownership, NAT, routing and IPv6 settings.
5. Compare the updated `aginctus gateway render` with each gateway instance's
   effective NICs. Existing PostgreSQL (and management-only LiteLLM) attachments
   may lack the explicit internal policy. Include the corresponding per-NIC
   `ipv4.gateway=none` update in the separately approved plan **before** changing
   the bridge or restarting those OCI guests. A management-only OCI attachment
   relying on bridge gateway discovery can otherwise restart without IPv4
   addressing when the bridge advertises `none`. Preserve every other NIC
   setting and record the exact prior presence/value for rollback. Compose may
   reject the changed NIC as recreation-requiring drift: do not enable recreation
   or delete/recreate a stateful database to bypass it. Review and authorize a
   targeted device update and, if necessary, that guest's restart instead.

For example, after manually assigning `network` to the verified bridge name and
`backup` to an existing private directory, these operations are read-only:

```sh
incus query "/1.0/networks/${network}?project=default" > "$backup/network.json"
incus network show "$network"
incus list --all-projects
```

Review saved configuration locally. Do not publish it. Re-read the network just
before mutation and compare it with the approved snapshot; abort on changed
ownership, settings or attachments.

## Separately authorized maintenance

Only after approval of the exact bridge update, backups, affected guests and
rollback plan, apply the single reviewed bridge setting:

```sh
incus network set "$network" ipv4.dhcp.gateway=none
incus network get "$network" ipv4.dhcp.gateway
```

Retain DHCP and DNS. Do not change NAT/routing, trust the bridge, flush firewall
tables, or modify the egress network. Existing guest leases/routes need not change
immediately: inspect each affected guest's DHCP client and route table. Renew the
lease with that client's documented procedure, or restart only a specifically
approved, backed-up instance during the window. Do not bulk restart workloads or
reattach NICs. Preserve disk/state devices, credentials and profiles. OCI guests
with explicit internal NIC policy may already have the correct route state;
inspect rather than restarting them speculatively.

Ensure the effective setting on the updated CLI/NixOS deployment is `none`, then
run `aginctus network ensure` and confirm idempotence. Restore any paused units
to their recorded state after verification. A NixOS switch alone does not renew
guest DHCP leases or reverse imperative Incus configuration.

## Verification and evidence boundaries

- Observe the actual managed OpenCode guest's IPv4 address, on-link management
  route and absence of any IPv4 default route. Verify Herdr's route as well.
- Rerun `nix --quiet develop -c just check preflight-report pilot-topology` from
  the reviewed checkout with the matching installed CLI. Retain its sanitized
  result; it is infrastructure preflight, not release signoff.
- Check bridge DNS, LiteLLM-to-PostgreSQL DNS/TCP readiness,
  workload-to-gateway TCP and Herdr-to-workload private connectivity separately.
- Follow the [guest TCP controls](acceptance-opencode-preflight.md) and
  [firewalld egress acceptance](gateway-firewalld-egress.md) for independently
  authorized direct-public, host, private-network and Tailscale denial checks.
  A kernel no-route result does not test what happens if a guest adds a route.
  Preserve distinct `not_verified` values for actual packet enforcement and
  provider inference until their own evidence exists.

Disposable validation uses `storage-network` for explicit bridge policy,
idempotence, immutable legacy drift rejection and foreign ownership rejection;
`runtime-lifecycle` verifies DHCP guest routes with the strict pilot parser and
Herdr-to-workload DNS/SSH transport. `gateway-topology` also exercises those routes,
PostgreSQL DNS/TCP, Herdr-to-workload SSH transport and workload-to-gateway TCP.
These scenarios never contact the pilot daemon or register a provider.

## Rollback

If maintenance fails, keep workloads stopped if required for data consistency
and restore only the saved gateway policy on the same re-verified owned bridge.
If the original key was absent, use `incus network unset "$network"
ipv4.dhcp.gateway`; otherwise restore its exact saved value with `incus network
set`. Restore any approved OCI NIC changes to their recorded prior values as
part of the same rollback plan. Renew/restart only the individually approved
guests again as needed and
compare routes and private connectivity with the saved baseline. Restore the
prior effective CLI/Nix settings/package as necessary so reconciliation does not
keep rejecting the restored legacy policy. Restore paused units to their saved
state. No rollback step enables NAT/routing or changes the host firewall.

Restoring the legacy DHCP advertisement also restores the original preflight
failure; record that explicitly. A NixOS generation rollback does not undo the
live bridge change or guest route renewal.
