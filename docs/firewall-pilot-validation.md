# v0.1 Incus DNS/DHCP host-firewall validation

This validation follows the automated disposable-VM checks for issue #220. Do **not** take
`incus list` showing 2/2 running as an HTTP/application readiness test.
Do not display expanded Incus configurations containing gateway secrets.
The management-interface default-route issue is separate.

## Read-only preflight (safe to share redacted summaries)

```sh
sudo nft -a list table inet firewalld | head -20 # when firewalld is enabled
sudo nft -a list ruleset | grep -E 'table |chain (filter_INPUT|INPUT|input)|hook input|reject' | head -100
incus network get aginctus-mgmt ipv4.address
incus network get aginctus-mgmt ipv4.nat
incus network get aginctus-mgmt ipv4.routing
incus network get aginctus-mgmt user.aginctus.managed
incus network get aginctus-mgmt user.aginctus.resource
aginctus network firewall policy
```

If firewalld owns `table inet firewalld` (`flags owner,persist`), use its
management API instead of writing nftables rules into the protected table.
A separate rejecting host base chain is an unsupported mixed-backend case;
stop rather than widening host access. The read-only diagnostics command in
PR #221 is independent of this stack and is not required for preflight.

## Mutating activation (operator authorization required)

On a NixOS host that has the generic `services.aginctus.enable` module,
activate the new module version with the normal host NixOS rebuild. Do
**not** add any `trustedInterfaces`, network-specific port allowances,
enable management NAT/routing or disable either host firewall.

```sh
# HOST FIREWALL MUTATION after reviewed NixOS activation:
sudo systemctl start aginctus-firewall-reconcile.service
# READ-ONLY verification:
sudo systemctl status aginctus-firewall-reconcile.service --no-pager
sudo nft list chain inet firewalld filter_INPUT
sudo firewall-cmd --zone=aginctus-dns --list-rich-rules
sudo firewall-cmd --zone=aginctus-dns --list-interfaces
```

On an iptables-nft host without firewalld, inspect `sudo iptables -S nixos-fw` and
`sudo iptables -S AGINCTUS_INPUT`.

## Guest DNS, PostgreSQL and HTTP readiness

Replace `10.242.246.1` with the *observed* live bridge IPv4 host address.
Never assume that literal address on other hosts.

```sh
# READ-ONLY guest probes; require a running LiteLLM container.
incus --project aginctus-gateway exec litellm-1 -- cat /etc/resolv.conf
incus --project aginctus-gateway exec litellm-1 -- getent hosts postgres-1.incus
# If dig is already installed inside the guest:
incus --project aginctus-gateway exec litellm-1 -- dig +time=2 +tries=1 @10.242.246.1 postgres-1.incus A
incus --project aginctus-gateway exec litellm-1 -- dig +tcp +time=2 +tries=1 @10.242.246.1 postgres-1.incus A
# Check that PostgreSQL is reachable by hostname, not a manually copied IP:
incus --project aginctus-gateway exec litellm-1 -- python3 -c 'import socket; s=socket.create_connection(("postgres-1",5432),3); s.close(); print("hostname TCP OK")'
# LiteLLM application liveliness (not merely Incus instance RUNNING):
incus --project aginctus-gateway exec litellm-1 -- python3 -c 'import urllib.request; print(urllib.request.urlopen("http://127.0.0.1:4000/health/liveliness",timeout=5).status)'
```

If `dig` is absent, run the UDP and TCP DNS checks from another disposable
guest on the same managed bridge with a resolver utility installed; do not
mutate the LiteLLM image only to add debugging packages.

## Negative tests and lifecycle

On a **disposable** Incus deployment (never delete or rename the live gateway
network to test the firewall), verify:

- an unrelated bridged interface with a misleading `aginctus-*` name never
  receives a host permission;
- a network with missing/incorrect `user.aginctus.*` markers, wrong role,
  wrong type, wrong host address, enabled NAT/routing or unsupported IPv6 is
  rejected; only Aginctus's owned rules are cleared;
- a known **listening but unauthorized** host TCP/UDP service is unreachable
  from the guest (a closed port does not prove firewall denial);
- DHCPv4 client leasing/renewal continues, DNS works on both UDP and TCP;
- changing/replacing/removing the selected owned bridge removes old grants
  after reconciliation; foreign rules and services remain unchanged;
- **HOST MUTATION:** reload firewalld or NixOS firewall and restart Incus,
  then confirm that the reconciler repairs only its owned INPUT permissions;
- reboot and repeat, and check `ipv4.nat=false` and
  `ipv4.routing=false` still hold.

The 30-second timer is a recovery backstop; the boot-time network ensure
also initiates an immediate firewall reconciliation when enabled. Until the next reconciliation,
a firewalld reload can deny DNS; do not create broader fallback exceptions.
Record the actual failure verdict and recovery behavior in the PR, rather
than marking acceptance complete from an untested design.
