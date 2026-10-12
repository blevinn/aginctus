# OpenAI OAuth host login (initial slice, #169)

The host CLI supports:

```sh
aginctus gateway provider openai auth --oauth
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

The host ID and session are stored under:

```text
$XDG_STATE_HOME/aginctus/providers/openai/
```

With the normal default of `~/.local/state/aginctus/providers/openai/`.
The files use 0600 permissions and parent directory 0700; the actual OAuth
access/refresh/ID tokens are never displayed by the command or included
in Nix configuration. A prior session is not replaced if reauthentication
fails identity/scope validation.

## Implementation and security review still required

- Security review of ID-token signature verification and callback state handling.
- Real live-browser compatibility test against the current dynamic
  registration flow; no credentials or callback secrets in transcripts.
- A safe account status/logout/revoke UX and atomic refresh-token rotation.
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
nix develop -c gofmt -w internal/provider/openai/oauth.go internal/provider/openai/oauth_test.go
nix develop -c gofmt -w internal/cli/run.go
nix develop -c go test ./...
nix flake check
```

Do not run `gateway apply` for the OAuth mode. The gateway implementation
currently requires an independent upstream API key.
