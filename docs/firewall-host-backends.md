# Incus DNS/DHCP on a NixOS host (issue #220)

An Incus bridge DNS listener and its nftables accept rules do not establish
guest reachability. Another INPUT base chain can reject packets after an
Incus chain accepts them. Never open an entire bridge or enable management
routing/NAT to resolve this issue.

The existing `services.aginctus` NixOS module is the intended one-time,
generic integration. On the pilot's iptables-nft backend,
`networking.firewall.extraCommands` is evaluated before NixOS's final
reject; it can insert a narrowly scoped Aginctus-owned chain *in the actual
NixOS INPUT path*. Native nftables has a different extension point and must
not silently use an iptables adapter. Membership must derive from authenticated
Incus network identity, not a network naming convention.

## Reproduce the packet drop

`aginctus network firewall diagnose` is **read-only**; it summarizes backend
evidence, not complete firewall rules or credential-bearing Incus instances.

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

## Firewalld-native INPUT adapter

A `filter_INPUT` chain at `priority filter + 10` is characteristic of
**firewalld**, not evidence that the iptables-nft backend itself enforces that
chain. Always inspect the enclosing table name; the pilot still needs a packet
trace to prove the rejecting chain. An iptables-nft ACCEPT does not defeat an
independent firewalld reject at a later hook priority.

When `services.firewalld.enable` is active, the generic Aginctus host module
does not add an iptables INPUT hook. Its privileged reconciliation instead
adds an ordinary `aginctus_dns_input` chain **inside `table inet firewalld`**
and an owned jump at the start of firewalld's actual `filter_INPUT` chain.
Reconciliation replaces the owned chain in one nft batch; it never edits
FORWARD/NAT or disables firewall filtering. A missing or disowned bridge
yields an empty chain. Firewalld reloads can recreate its table and erase
runtime hooks: systemd starts the reconciler after firewalld, and a
30-second timer provides eventual recovery. There may be a short **DNS outage**
between reload and reconciliation; no broad fallback is permitted.

This targets firewalld's currently observed nft layout. If the chain or
terminal reject cannot be verified, reconciliation refuses modifications.
The integration must be validated on the pilot host before declaring #220
complete. Unrelated instance default-route problems are out of scope.
