# Managed gateway OAuth credential storage (#171)

This is the **first provisioning slice** of #171; it is not yet an OAuth
token-sharing mechanism.

## Provisioning

Run locally on a trusted NixOS host:

```sh
aginctus gateway provider openai storage ensure
```

The configured `gateway.oauth.storage.pool` defaults to `default`.
The gateway ID defaults to `local`, giving a custom Incus volume:

```text
default/custom/aginctus-gateway-local-oauth
```

It lives in the Incus **default** project. The name and storage pool are
validated before touching Incus. If missing, Aginctus creates a filesystem
volume marked:

```text
user.aginctus.managed=true
user.aginctus.resource=gateway-oauth-credentials
user.aginctus.gateway=local
```

If it already exists, Aginctus validates **all** markers and verifies the
expected custom/filesystem type. A same-named volume with different metadata
is rejected. Repeated ensures preserve the existing volume contents;
no cleanup path is added here.

This command creates an **empty, unmounted persistent volume** only. It does
not copy credentials, start a gateway service, connect tokens to LiteLLM, or
alter Herdr/OpenCode networking. It is not yet part of `gateway apply` or
aggregate apply. Do not store or transfer actual OAuth tokens there manually.

## Pending integration before user credentials can move

- A dedicated gateway-owned OAuth service/container (not LiteLLM or
  PostgreSQL) and a mount of the custom volume on its single private state
  path. Ownership, guest filesystem permissions, and immutable service
  settings must be verified on real Incus.
- A secure account-scoped handoff protocol from the authenticated host
  CLI to the gateway; reject unauthenticated writers and any duplicate
  active refresh-token owner. The transfer must not pass OAuth refresh tokens
  on process command arguments or into a rendered plan.
- A durable atomic importer with crash-consistent acknowledgement,
  coordinated deletion/relinquishment of host token ownership, and safe
  handling of failed handoffs. Explicitly test concurrency, restart,
  rollback, and recovery.
- Gateway-side refresh serialization, account-scoped access controls,
  local logout/revocation, and audit-safe logging.
- Compose/Jsonnet changes to mount the volume **only** into the dedicated
  runtime; tests for absence from LiteLLM, PostgreSQL, Herdr, and OpenCode.
- Integration with the restricted OAuth Responses adapter tracked by #172.

## Validation

```sh
nix develop -c go test ./...
nix flake check
nix develop -c go run ./cmd/aginctus config get gateway.oauth.storage.pool
nix develop -c go run ./cmd/aginctus gateway provider openai auth status
```

The last two commands are read-only. After code review and only with
operator approval, volume creation can be smoke-tested with
`aginctus gateway provider openai storage ensure`, followed by a read-only
`incus storage volume show default aginctus-gateway-local-oauth`. Do **not**
run that mutating command as a prerequisite to checking the Go/Nix tests.

### Branch / PR relationship

This work is based on PR #170's OAuth branch and must land *after*
or alongside #170. It does not depend on #160's alternative API-key gateway
provider configuration.
