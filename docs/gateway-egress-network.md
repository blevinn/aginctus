# Gateway-only outbound bridge

Start with the [gateway and egress operator decision path](gateway-operator-path.md) for ordered read-only inventory, exceptional maintenance, credential approval, inference evidence and key revocation. This page describes the configured network contract, not live acceptance of guest routes or host filtering.

The gateway egress bridge gives LiteLLM an outbound path while keeping
PostgreSQL and agent workloads on the private management network. Network
reconciliation does not configure providers or distribute credentials.

## Behavior

The default effective configuration uses
`gateway.egress.network.name = "aginctus-egress"`. The gateway orchestration
ensures an Aginctus-owned Incus bridge with IPv4 NAT and IPv4 routing enabled,
IPv6 disabled, and fail-closed ownership metadata. It executes **after**
the existing non-NAT management network and **before** Compose reconciliation.

Only LiteLLM gains the outbound bridge; PostgreSQL remains connected to
the management network alone. The Herdr and OpenCode workloads remain on the
unrouted, non-NAT management network. The egress bridge does **not** open a
workload-to-provider egress route.

Compose references both bridges as external networks; Aginctus reconciles
them before Compose runs. LiteLLM's management attachment is marked
`x-incus-compose.internal` so it does not supply the default route. The
actual guest route and host isolation still require deployment validation.

The effective configuration can specify the egress bridge CIDR, LiteLLM
host CIDR, and DHCP policy. See [configuration](configuration.md) for
configuration sources and [the OCI egress pilot](litellm-oci-egress-pilot.md)
for address selection, restricted DHCP ranges, stopped-instance migration,
recovery, and live route/isolation acceptance. Existing instances are not
proven migrated by a rendered plan.

At the Go API boundary, an empty `gateway.Spec.EgressNetwork` omits the
extra bridge. The default CLI configuration populates it. A nonempty name
must be valid and differ from the management network.

## Validation

From the repository root, these checks do not provision Incus resources.
Go tests exercise local contracts; Nix checks build the configured flake
checks, not the separately opt-in live pilot:

```sh
nix --quiet develop -c go test ./internal/gateway ./internal/cli
nix --quiet flake check
nix --quiet develop -c go run ./cmd/aginctus gateway validate
nix --quiet develop -c go run ./cmd/aginctus gateway render
git diff --check
```

Check render output for LiteLLM's `management` and `egress`
networks; PostgreSQL must list only `management`.

Real Incus smoke testing of bridge ownership, network isolation, and
reconciliation belongs to deployment validation. `gateway apply` **mutates
running infrastructure**, and is not part of this validation command list.

## Related contracts

- [API-key provider pilot](gateway-api-key-pilot.md): provider registration
  and the separately authorized live path.
- [Native ChatGPT OAuth](architecture/native-chatgpt-oauth.md): accepted
  credential design; OAuth tokens are not interchangeable with API keys.
- [Workload key pilot](gateway-workload-key-pilot.md): scoped LiteLLM key
  provisioning and revocation, separate from upstream credentials.
- [Host firewall policy](firewall-host-backends.md): management isolation
  and DNS/DHCP integration.

Rendered topology and component tests do not establish successful upstream
inference. Record live egress acceptance on
[#257](https://github.com/blevinn/aginctus/issues/257) and follow the separate
[#122 inference gate](https://github.com/blevinn/aginctus/issues/122),
including its operator hold, before model-backed testing.
