# OpenAI OAuth host login (initial slice, #169)

The host CLI supports:

```sh
aginctus gateway provider openai auth --oauth
aginctus gateway provider openai auth --oauth --new
aginctus gateway provider openai auth status
aginctus gateway provider openai auth select <account-id>
aginctus gateway provider openai auth refresh
aginctus gateway provider openai auth logout
```

This is a **draft sign-in implementation** using OpenAI's supported
[Sign in with ChatGPT open-source OAuth flow](https://developers.openai.com/siwc/token-sharing-open-source/sign-in).
It is not yet connected to the deployed gateway and does **not** make
inference available. Do not substitute OAuth tokens for the existing
LiteLLM API-key provider on #160.

## Sign-in

Run from the trusted NixOS **host** with a browser on that same host.
Aginctus registers its own dynamic public client, opens a callback listener
on `127.0.0.1` only, generates fresh PKCE, state, and nonce values,
exchanges the issued client ID and authorization code, validates the
OpenAI-signed ID token, and verifies the required ChatGPT plan scope.
On headless hosts it prints an authorization URL for opening in a browser
on the *same machine*. No listener is opened in the OpenCode workload.

The host ID, active selection, and separate per-registration session files are stored under:

```text
$XDG_STATE_HOME/aginctus/providers/openai/
```

With the normal default of `~/.local/state/aginctus/providers/openai/`.
On first subsequent authentication or account-management operation, an earlier
single-session `session.json` record is migrated under the per-account store
and the legacy secret copy is retired. This migration is protected by the
account lock and is designed to be idempotent.

Credential records are under `accounts/<sha256(client_id + NUL + subject)>.json`,
with an owner-only `active-account` pointer. Use `auth --oauth --new` to
register another account; the ordinary `--oauth` flow reauthorizes the
currently selected registration. `auth status` prints identifying account
metadata only and never token material. `auth logout` attempts refresh-token
revocation through OpenAI OIDC discovery, retains account registration but
clears local credentials, and explicitly reports unconfirmed revocation.
`auth refresh` serializes updates under a host-local advisory file lock and
atomically replaces rotating refresh tokens. If the gateway eventually owns
a token after import, the host CLI must **stop** refreshing it (see #171).

The files use 0600 permissions and parent directory 0700; the actual OAuth
access/refresh/ID tokens are never displayed by the command or included
in Nix configuration. A prior session is not replaced if reauthentication
fails identity/scope validation.

## Implementation and security review still required

- Security review of ID-token signature verification and callback state handling.
- Real live-browser compatibility test against the current dynamic
  registration flow; no credentials or callback secrets in transcripts.
- Integration tests for remote revocation and token refresh responses,
  crash-safe migration from legacy single-session records, and concurrency
  under more than one host process. Basic status, logout/revocation,
  serialized refresh, and account selection are implemented.
- Per-user account selection where multiple OAuth clients exist.
- A scoped **Responses-only** adapter with `store:false`, `stream:true`,
  and real `response.completed` validation; the default LiteLLM
  `gpt-4o-mini` Chat Completions provider is not an OAuth integration.
- Compliance check: the [SIWC terms](https://openai.com/policies/sign-in-with-chatgpt-terms/)
  prohibit turning a user's subscription into a general-purpose API for
  unrelated tools. An Aginctus-controlled agent integration must not
  expose arbitrary third-party forwarding or pool credentials.

## Local validation

```sh
nix develop -c gofmt -w internal/provider/openai/*.go
nix develop -c gofmt -w internal/cli/run.go
nix develop -c go test ./...
nix flake check
```

Do not run `gateway apply` for the OAuth mode. The gateway implementation
currently requires an independent upstream API key.

## Deployment follow-ons

- #171 provisions a managed Incus OAuth credential volume, protected handoff,
  and a sole gateway runtime refresh-token owner. No such volume exists yet.
- #172 provides the restricted, account-authorized Responses API adapter.
- #160 remains a separate API-key deployment; signing in here does not
  provision credentials to LiteLLM or enable inference.
