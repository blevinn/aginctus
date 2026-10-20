# LiteLLM egress with firewalld

NixOS owns the durable security boundary. Aginctus owns narrowly scoped runtime
permissions inside it, using firewalld's native D-Bus API. Incus owns bridge
addressing, routing and NAT. Aginctus never reloads firewalld, changes permanent
objects, assigns a trusted zone, or installs a second host filtering table.

## Ownership and evaluation order

Declaring an egress bridge on a firewalld NixOS host automatically enables
`services.aginctus.firewall.egress.enable`. Initial activation provisions:

| Object | Permanent definition | Runtime changes |
| --- | --- | --- |
| Zone `aginctus-egress` | DROP; configured bridge binding; no services or forwarding | None |
| Policy `aginctus-egress-out` | Egress zone to ANY, DROP, priority -500 | None |
| Policy `aginctus-egress-in` | ANY to egress zone, DROP, priority -500 | None |
| Policy `aginctus-egress-input` | Egress zone to HOST, DROP, priority -500 | None |
| Policy `aginctus-egress-https` | Egress ingress zone, no egress binding, CONTINUE, priority -1000; dormant | Named uplink and source-specific IPv4 rich rules |
| Policy `aginctus-egress-host` | Egress zone to HOST, CONTINUE, priority -1000; no grants | Narrow IPv4 DNS/DHCP rich rules |

ANY excludes HOST. The separate HOST policy therefore matters. The HTTPS policy
accepts only the verified LiteLLM IPv4 source to TCP/443 through the configured
uplink zone. Earlier rich-rule drops exclude non-public ranges and every
nondefault IPv4 route from all host routing tables, including host-local public
addresses and connected public networks. This conservative policy can also
exclude a public endpoint reached through a more-specific route.

The VM test's actual nftables output places the HTTPS policy before the outbound
DROP policy in `filter_FORWARD_POLICIES`. The rich-rule ACCEPT terminates that
firewalld forwarding traversal. An ACCEPT in an independent earlier base chain
would not bypass a later rejecting firewalld chain. Positive and negative packet
tests cover the actual path, not just generated text.

The dedicated zone uses minimum ingress/egress classification priority so its
interface binding precedes ordinary overlapping source-based zones, including
trusted sources. Other administrators must not introduce competing minimum
priority classifications or earlier accepting policies for this boundary.

The integration also requires these host-wide firewalld settings:

- `CleanupOnExit=false`: retain filtering while the daemon is stopped. Restart
  must not temporarily leave forwarding unfiltered.
- `StrictForwardPorts=true`: prevent the early conntrack-DNAT shortcut from
  accepting traffic before the directional policies. Existing published ports
  that depend on implicit Docker/Incus DNAT acceptance need review before host
  activation.
- nftables backend and table ownership, with flowtable bypass disabled.

These requirements are checked both at NixOS evaluation and through D-Bus before
runtime mutation. NixOS additionally requires DROP during reload and forbids a
host-wide nftables flush/raw ruleset. Do not add preempting foreign ACCEPT policies for the egress
zone. Root administrators are concurrent authorities and must serialize host
firewall maintenance; the lifecycle lock cannot constrain unrelated root tools.

Existing management-network policy is unchanged. LiteLLM intentionally has a
second NIC for same-L2 PostgreSQL access, with its management default gateway
suppressed. Routed egress filtering does not deny that database dependency or
provide mutual isolation on the management L2. Additional ordinary workloads
remain management-only. The provider bridge is single-tenant: inventory must
show only the owned LiteLLM NIC, including parent-based bridge attachments, so
another guest cannot spoof its permitted source.

One declared provider bridge is supported per host integration. Its name and
permanent binding are bootstrap topology. Changing that topology requires a
reviewed declarative activation. Routine provider, workload, source-address and
uplink changes do not require new host firewall exceptions or rebuilds.

## Runtime lifecycle

`aginctus-egress-firewall.service` reconciles every five seconds, with bounded
timer coalescing. Management DNS/DHCP reconciliation uses the same interval when
egress is enabled. Reload restores the empty permanent grants; successful
reconciliation repairs them without another reload. Service execution and failed
discovery can extend that interval; this is not zero-interruption DNS availability.

Before granting access, reconciliation checks:

- Complete structured permanent and runtime settings for all six objects. Only
  known neutral defaults are accepted outside the ownership contract. Unknown
  fields, helpers, source bindings, ports, ICMP inversion and other drift fail.
- The root-owned previous/pending grant record, allowing recovery from partial
  D-Bus writes or an interrupted process, and the current firewalld bus owner.
- Live Incus bridge identity, ownership, NAT/routing, IPv6 disablement, actual
  Linux bridge address, bounded/disabled DHCP, and all-project address inventory.
- Exact LiteLLM ownership and its two NIC declarations, with no extra attachment.
- A unique IPv4 default-route interface belonging to the explicit uplink zone.

The operation is serialized by `/run/aginctus-firewall/lock`. It withdraws old
grants before changing scope, prepares the complete HTTPS rule set while its
policy has no egress binding, and activates it by binding the single named uplink
in one native firewalld transaction. A concurrent reload resets the rules and
binding to empty, never ANY, preventing a reload from widening a grant's scope.
Discovery is repeated before and after activation. On mutation/discovery
failure it attempts withdrawal with an independent bounded rollback context.
Foreign ownership or invalid permanent structure is refused without taking over
those objects. Runtime drift in verified owned objects withdraws our rich rules
and reports an error; it does not silently adopt structural changes.

Firewalld accepts established connections before policy evaluation. Withdrawal
therefore also deletes only the recorded sources' IPv4 TCP/443 conntrack entries.
There is no global conntrack flush. Conntrack failures are reported as incomplete
withdrawal, never successful rollback. Bridge/instance removal or failed identity
discovery withdraws grants; the durable bridge binding and DROP policies remain.

Root-managed `/var/lib/aginctus-firewall/overrides.json` initially contains `{}`.
It overlays `/etc/aginctus/config.json` for this service only. For example:

```json
{"firewall":{"egress":{"uplinkZone":"nixos-fw-default"}}}
```

An empty uplink fails closed. Operators can update this file, or add a changed
`gateway.egress.ipv4.address` after reconciling the actual NIC, without rebuilding
NixOS. Runtime credentials do not belong in this file. The timer does not deploy
gateways or change provider configuration.

In an authorized host maintenance context:

```sh
aginctus --configuration-file /var/lib/aginctus-firewall/overrides.json network firewall egress reconcile
# Stop the timer first if withdrawing a still-running gateway deliberately.
systemctl stop aginctus-egress-firewall.timer
aginctus --configuration-file /var/lib/aginctus-firewall/overrides.json network firewall egress revoke
```

`revoke` leaves the default-deny boundary installed. Re-enabling the timer grants
access again only after successful verification. Removing the entire NixOS
integration is different: withdraw grants and stop/remove egress guests first,
then retire the bridge or keep it quarantined in host policy.

## Diagnostics and retired commands

`gateway egress firewall plan --uplink-zone=ZONE` uses the same canonical rule
generator but has no live route/ownership inventory. It is advisory.
`gateway egress firewall audit --uplink-zone=ZONE` is a legacy name/detail report,
not proof of the complete six-object boundary.

`gateway egress firewall reload-parity` compares selected runtime/permanent CLI
listings, ignoring active/default header annotations. It is diagnostic only. It
does not inspect all direct rules, daemon settings, service/IP-set contents or
cross-manager state, and never authorizes reload. Healthy runtime grants can
make it report differences.

`stage`, `verify-staged` and `unstage` are retired and return a migration error.
There is no verification-to-permanent-deletion race: Aginctus no longer deletes
NixOS-owned objects. Preexisting imperative staging must be reviewed before
bootstrap; it is not automatically adopted or removed.

## Verification and controlled activation

Safe repository checks:

```sh
nix --quiet develop -c go test ./...
nix --quiet develop -c go vet ./...
nix --quiet flake check
nix --quiet build --no-link .#firewall-firewalld-integration
git diff --check origin/main...HEAD
```

The disposable VM uses real firewalld, D-Bus, nftables, Incus bridges and network
namespace peers. Synthetic gateway discovery avoids downloading an OCI image;
separate Go tests cover production Incus/default-route discovery. It exercises
HTTPS source/port restrictions, private and connected-public denial, trusted-zone
ingress/egress, DNAT bypass denial, host service denial, IPv6 denial, DNS TCP/UDP,
DHCP on both bridges, PostgreSQL L2 connectivity, management-only Internet denial,
runtime drift refusal, established-flow withdrawal, timer recovery after reload,
retained filtering while stopped, restart, and reboot. The result retains
`egress-nftables.txt` as generated-rule evidence.

Build the host candidate without elevation, using an isolated snonix checkout
and this worktree as the Aginctus input. Do not re-pin merely to test the CLI:

```sh
nix --quiet build --no-link --print-out-paths --accept-flake-config \
  --override-input aginctus path:/tmp/opencode/aginctus-371 \
  /tmp/opencode/snonix-371-activation#nixosConfigurations.boffo.config.system.build.toplevel
```

Before activation, review the candidate closure/unit diff, published-port impact,
and the operator's SSH/Tailscale recovery path. Obtain explicit human approval.
The activation changes firewalld configuration and may reload/restart it. Nothing
in the repository checks authorizes this action.

For a temporary pilot, record `readlink -f /run/current-system`, then invoke the
candidate's `bin/switch-to-configuration test` from an authorized root terminal.
This activates the NixOS bootstrap without changing the boot generation. Keep
the uplink override empty until inspecting the six objects, then set the named
uplink and start reconciliation. Verify DNS and forced-IPv4 TLS, prohibited
private/host-local access, management-only Internet denial, actual PostgreSQL
connectivity, management DHCP/DNS, SSH and Tailscale. Authenticated Responses
inference is separate and requires ready credentials/routing and cost approval.

The minimum rollback is stopping the egress timer and invoking `revoke`, leaving
the deny boundary in place. If the host activation itself must be reverted,
stop LiteLLM first, then run the recorded previous system's
`bin/switch-to-configuration test` and reconcile its management DNS/DHCP service.
The prior boot generation remains selected. Keep the gateway stopped until its
network isolation is reviewed again. No instance, volume, image or secret
deletion is required.
