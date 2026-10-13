# Gateway-only outbound bridge

This PR extracts **only the gateway egress-network capability** from draft #160.
It does not configure an upstream provider, require an OpenAI API key,
enable OAuth, or distribute workload credentials.

## Behavior

The default effective configuration uses
`gateway.egress.network.name = "aginctus-egress"`. The gateway orchestration
ensures an Aginctus-owned Incus bridge with IPv4 NAT and IPv4 routing enabled,
IPv6 disabled, and fail-closed ownership metadata. It executes **after**
the existing non-NAT management network and **before** Compose reconciliation.

Only LiteLLM gains the outbound bridge; PostgreSQL remains connected to
the management network alone. The Herdr and OpenCode workloads remain on the
unrouted, non-NAT management network. This change does **not** open a
workload-to-provider egress route.

For compatibility with existing direct `gateway.Spec` callers in tests,
an empty `Spec.EgressNetwork` omits the extra bridge. The default config
populates it for normal CLI operations. A nonempty name is validated and
must differ from the management network.

## Validation

Without provisioning anything:

```sh
nix develop -c gofmt -w internal/gateway/egress_test.go
nix develop -c go test ./...
nix flake check
nix develop -c go run ./cmd/aginctus gateway render
git diff --check
```

Check render output for LiteLLM's `management` and `egress`
networks; PostgreSQL must list only `management`.

Real Incus smoke testing of bridge ownership, network isolation, and
reconciliation belongs to deployment validation. `gateway apply` **mutates
running infrastructure**, and is not part of this validation command list.

## Next PRs

- A focused provider-routing PR must handle the API-key path separately
  (remaining scope of #160) and must not treat SIWC tokens as API keys.
- A separate OAuth integration PR must establish dedicated daemon storage,
  secure token handoff, and a LiteLLM-backed Responses adapter.
- Workload identity/key issuance stays deferred to v0.2 (#177).
