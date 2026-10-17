# Incus DNS/DHCP on a NixOS host (issue #220)

An Incus bridge DNS listener and its nftables accept rules do not establish
guest reachability. Another INPUT base chain can reject packets after an
Incus chain accepts them. Never open an entire bridge or enable management
routing/NAT to resolve this issue.

The existing `services.aginctus` NixOS module is the intended one-time,
generic integration. On a NixOS iptables-nft backend,
`networking.firewall.extraCommands` is evaluated before NixOS's final
reject; it can insert a narrowly scoped Aginctus-owned chain *in the actual
NixOS INPUT path*. Native nftables has a different extension point and must
not silently use an iptables adapter. Membership must derive from authenticated
Incus network identity, not a network naming convention.

## Reproduce the packet drop

The separate diagnostic command proposed in PR #221 is not part of this
workstream; use the read-only host commands below until it is merged.

These initial commands are also **read-only**:

```sh
iptables --version
sudo nft -a list ruleset | grep -E 'table |chain (filter_INPUT|INPUT|input|input-allow)|hook input|reject' | head -80
incus network get aginctus-mgmt ipv4.address
incus network get aginctus-mgmt user.aginctus.resource
```

For packet-level evidence, the following **mutates the host firewall
temporarily only to set an nft trace bit**. Verify the actual bridge name and
address first, use a second terminal for `sudo nft monitor trace`, then
generate guest queries using `dig +tries=1 +time=2 @10.242.246.1
postgres-1.incus A` and `dig +tcp @10.242.246.1 postgres-1.incus A`:

```sh
sudo nft add table inet aginctus_trace
sudo nft 'add chain inet aginctus_trace pre { type filter hook prerouting priority -301; policy accept; }'
sudo nft 'add rule inet aginctus_trace pre iifname "aginctus-mgmt" ip daddr 10.242.246.1 udp dport 53 meta nftrace set 1'
sudo nft 'add rule inet aginctus_trace pre iifname "aginctus-mgmt" ip daddr 10.242.246.1 tcp dport 53 meta nftrace set 1'
sudo nft monitor trace
# Mandatory cleanup, including after an interrupted test:
sudo nft delete table inet aginctus_trace
```

Identify the **last** reject/drop and its table/chain/handle. Do not share
expanded gateway instance configuration, tokens or complete host rulesets.
LiteLLM's incorrect default route is a separate concern.

## Firewalld-managed interface zone

Firewalld's `table inet firewalld` has `flags owner,persist` on the pilot
configuration. External `nft -f` operations against that table fail even
with `CAP_NET_ADMIN`; the previous direct-nft approach is **unsupported**.
Do not disable table ownership or create an independent nftables accept base
chain.

The Aginctus NixOS module declares one empty, **DROP-by-default**
zone named by `services.aginctus.settings.firewall.zone` (default
`aginctus-dns`), materialized as `/etc/firewalld/zones/<zone>.xml`.
Firewalld zone names are limited to 17 characters; Aginctus requires the
`aginctus-` prefix and lowercase alphanumeric/hyphen suffix. For example:

```nix
services.aginctus.settings.firewall.zone = "aginctus-lab";
```

Using a custom name is supported today and tested with `aginctus-vm`. Future
multi-profile support should allocate **one different zone per profile** and
ensure corresponding distinct units and network ownership policies. Multiple
Aginctus instances or profiles on the same host are **not** enabled by this
option alone (planned for v0.3). Do not configure multiple independent
instances to share a zone.

The zone has **no permanent interface bindings and no permanent
allowances**. The privileged
`aginctus network firewall reconcile` command calls `firewall-cmd` to:

1. Confirm the dedicated zone exists and is DROP-only, with no unexpected
   services, sources, ports, protocols or forward-ports.
2. Verify current Incus ownership. Leave an unchanged, matching policy
   intact to avoid disrupting DNS every 30 seconds; otherwise remove stale
   runtime exceptions before installing anything new. On failed verification,
   leave the exceptions empty (fail closed) and return a nonzero status.
3. Verify the selected Incus network's ownership labels, isolation flags,
   bridge interface, subnet, and configured name/address.
4. Add source/subnet-and-destination-constrained UDP/TCP DNS 53 rich rules
   and interface-scoped DHCPv4 UDP 67, then bind **only the verified bridge**
   to the dedicated zone. Unrelated interface bindings are rejected.
5. Reconcile again at system startup and on the existing 30-second timer.
   The NixOS systemd unit pins `firewall.backend=firewalld` and waits up to
   ten seconds for firewalld's D-Bus service and nftables INPUT chain to be
   ready. When the table is temporarily absent during a restart, the
   reconciler **fails closed** instead of falling back to iptables-nft.
   Firewalld reload clears runtime bindings/rules; an outage is possible
   until reconciliation restores them.

**DHCP qualifier:** firewalld rich-rule syntax cannot simultaneously match
UDP source port 68 and destination port 67. The DHCP permission therefore
matches destination port 67 on the verified *bridge interface*, without a
source-port constraint. It is narrower than a trusted interface or arbitrary
host-port allowance, but is not equivalent to the earlier iptables-nft
source-port-68 matcher. The production deployment must retain the default
DROP zone and be assessed against this distinction.

The dedicated zone owns only runtime rules, not NAT or forwarding. It never
calls `--runtime-to-permanent`, `--change-interface`, or `--reload`. If the
bridge becomes disowned, its restrictive DROP binding may remain until a
firewalld reload; that prevents a disowned bridge from suddenly inheriting a
more permissive default zone.

The automated `just test-nix` suite checks a *real* Incus daemon,
firewalld's owner-protected table and generated rules, UDP/TCP DNS from a
namespace client, blocked unrelated TCP, ownership revocation, reload and
cold-boot restoration. It does not mutate the pilot firewall.

## Pilot verification after the automated suite passes

Read-only (the commands below use the default zone; substitute the
configured name when customized):

```sh
sudo firewall-cmd --get-zones
sudo firewall-cmd --permanent --zone=aginctus-dns --get-target
sudo firewall-cmd --zone=aginctus-dns --list-all
sudo firewall-cmd --zone=aginctus-dns --list-interfaces
sudo firewall-cmd --zone=aginctus-dns --list-rich-rules
sudo nft -a list chain inet firewalld filter_INPUT
```

Do not proceed with a pilot firewall activation until the automated VM
integration and host backend review have passed.
