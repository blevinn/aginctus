# Dedicated OAuth gateway daemon — first executable slice (#170 / #171)

> **Superseded for inference activation.** This document describes the
> dedicated OAuth daemon approach that #170 / #171 explored. The
> accepted v0.1 architecture (see
> [`docs/architecture/native-chatgpt-oauth.md`](architecture/native-chatgpt-oauth.md))
> pivots to native LiteLLM ChatGPT provider integration; a dedicated
> daemon is **not** the v0.1 inference path. The daemon artifacts
> remain in the tree as opt-in infrastructure for a future v0.3
> separation; do not enable or activate them as part of v0.1 delivery.

This is an **isolated fail-closed runtime**, not a working OAuth inference
gateway. It establishes the guest service boundary before credentials are
transferred or LiteLLM is allowed to route requests to it.

## Artifacts

- `cmd/aginctus-oauth-gateway`: a standalone, single-purpose Go binary.
- `internal/oauthgateway`: private directory checks, loopback-only liveness,
  and explicit HTTP 503 on inference, import, or readiness endpoints.
- `nix build .#oauth-gateway`: a NixOS Incus guest image with an unprivileged
  `aginctus-oauth` service identity. It is **not referenced by the existing
  gateway Compose deployment** and does not change any running resources.

The service requires `/var/lib/aginctus/oauth` to be a real mountpoint and
requires ownership by the daemon UID and owner-only permissions. If the
custom volume is missing, systemd must not start it. The service has only
a loopback liveness socket (`127.0.0.1:4081`); it exposes no inference,
token import, or refresh endpoint. Liveness `/healthz` returns 200 even
while *all* inference endpoints return 503. The 200 is **not** readiness.

## Validation before any live deployment

```sh
nix develop -c go test ./...
nix flake check
nix build .#oauth-gateway
nix develop -c go test -race ./internal/oauthgateway
git diff --check
```

Do not deploy the guest or transfer any OAuth secret yet.

## Required next integration slices

1. Reconcile #160's network changes safely. The dedicated daemon must own a
   configured egress NIC; LiteLLM and PostgreSQL must not gain access to the
   credential volume.
2. Reconcile the #171 custom-volume lifecycle into `gateway apply` as a
   preflight. Attach the owned volume *only* to the daemon via an explicit
   Incus disk device and verify the host/guest storage project/pool, guest
   mount path, UID mapping, and restrictive filesystem permissions.
3. Implement daemon-side durable credential storage, an authenticated local
   administration/import channel, and the exclusive refresh-token ownership
   state machine in the architecture document. Host refresh must fail closed
   after handoff; crash recovery must be idempotent.
4. Implement a distinct authenticated LiteLLM-to-adapter inference interface
   and #172's strict streaming Responses semantics. LiteLLM remains the
   **only** workload-facing gateway; a static shared LiteLLM key is acceptable
   for v0.1, dedicated keys deferred to v0.2 (#177).
5. Verify real OpenAI sign-in, refreshed inference and OpenCode tool calls
   before promoting PR #170 to merge-ready.

Neither `aginctus gateway apply` nor aggregate apply provisions or enables
this daemon yet. The `gateway credential-storage ensure` subcommand only
creates empty storage and is not evidence of a functioning OAuth provider.
