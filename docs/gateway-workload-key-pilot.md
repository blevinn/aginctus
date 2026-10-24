# Pilot LiteLLM workload key lifecycle (#122)

**Stage 4** of the [gateway operator decision path](gateway-operator-path.md#3-provider-registration-and-key-handling). Issue a temporary LiteLLM key only for an operator-approved, already registered alias, and plan confirmed revocation before any paid request; an uncertain revocation must be treated as outstanding access.

This operator-only utility completes the credential delivery **between** the
merged guarded model registrar (#247) and authenticated Responses smoke test
(#248). It is **not** a general workload-identity system (deferred to #177 in
v0.2) and does not enable ChatGPT OAuth. Its only purpose is to manage a
short-lived key for one operator-approved API-key-backed LiteLLM model alias
already registered in the live gateway.

The integration is based on the pinned LiteLLM **v1.103.0** source:
`/key/generate` takes `models`, `duration`, `max_budget`,
`max_parallel_requests`, `allowed_routes`, and `key_alias`, and
`/key/delete` accepts a list of keys. This utility **does not**
generate upstream provider keys; it stores only the LiteLLM workload token.
It does not restart any guest, rewrite Compose, change DNS, or open a network.

## Safe defaults and limits

`scripts/gateway/manage-workload-key.py` has three modes:

- **Default**: non-network dry-run. Checks only syntax and an absolute output
  path; reads no files and creates no directories.
- **`--issue`**: explicitly contact the *private IPv4* LiteLLM endpoint,
  check the approved alias exists, and POST one `/key/generate` request.
  The generated key is restricted to **exactly one selected model alias**,
  one-hour expiry, **$1 total max budget**, `max_parallel_requests=1`,
  `rpm_limit=5`, and only the inference-route preset and exact `/v1/models` discovery route
  needed for the smoke-test flow. The response must explicitly confirm the exact
  model list, bounded budget and expiration; then a restricted
  `/v1/models` check must expose *only* the selected alias.
- **`--revoke`**: use the existing gateway master solely to POST
  `/key/delete` with the token from the protected file; remove that file
  **only after** a follow-up `/v1/models` probe rejects the deleted key
  with HTTP 401/403. On failure, the file remains for retry/review.

The secret file must be an **absolute path** outside `/nix/store`,
in a caller-owned **0700 directory**, and must not exist before issuance.
It is created with `O_CREAT|O_EXCL|O_NOFOLLOW` and mode 0600. Credentials
are read only from caller-owned, mode-0600 (or stricter), non-symlink
regular files. The gateway master is loaded from existing protected
`XDG_STATE_HOME/aginctus/gateway/local/secrets.json` state. A generated
key or admin secret is never printed, serialized into Git/Nix/Compose,
or sent to a workload. The script does **not** retry an ambiguous
`/key/generate` call: an unreachable response may still have created a
key remotely. Inspect the generated, nonsecret `aginctus-pilot-*` alias
using authenticated gateway administration and revoke it explicitly
before any retry. An issued key is retained locally if a subsequent
model-scope check fails, so it can be revoked.

**Important:** the pilot's private management HTTP endpoint carries a
bearer key in plaintext on the isolated bridge. Invoke the tool only
on the trusted pilot host; never expose, port-forward or run from a
workload. The management bridge must keep `ipv4.nat=false`,
`ipv4.routing=false`. Confirm that the *gateway-only* egress bridge
carries provider TLS traffic before declaring all-up acceptance.
The reported pilot baseline (2026-10-17) had a Running/ready LiteLLM
(`/health/liveliness` and `/health/readiness` both HTTP 200) with
private IP `10.144.44.70`; re-observe this address before live use.

## Operator commands

Offline preview (no network and no key-file preconditions):

```sh
nix develop -c python3 scripts/gateway/manage-workload-key.py \
  --endpoint http://10.144.44.70:4000 \
  --alias aginctus-approved-model \
  --output /absolute/private/workload.key
```

The alias above is **illustrative**, not a confirmed registered model.
Do not use `openai/placeholder` from #247's preview. Before issuance:
select a **real approved Responses-capable provider/model**;
use #247's guarded explicit `--apply` if it is not already registered;
confirm the instance is running and ready; and review the provider
billing/terms and private trust boundary.

Only after deliberate operator approval, create a private directory
outside Git and the Nix store (no commands here print keys):

```sh
install -d -m 0700 "$HOME/.local/state/aginctus/pilot-keys"

# Replace with an ACTUALLY REGISTERED, APPROVED LiteLLM model alias.
nix develop -c python3 scripts/gateway/manage-workload-key.py \
  --endpoint http://10.144.44.70:4000 \
  --alias aginctus-approved-model \
  --output "$HOME/.local/state/aginctus/pilot-keys/workload.key" \
  --issue
```

`--issue` is a *real gateway mutation*. It can create a billable
credential, but does not itself perform a provider model inference.
Successful output reports only nonsecret key alias, not token bytes.
For #248, pass the protected key file as `--workload-key-file`
and the real model alias as `--alias`. The smoke test's `--run`
is separately billable and separately authorized; first validate
the smoke tool's own offline test, negative key rejection and
trusted provider-side evidence.

Revoke the key as soon as the pilot acceptance is complete:

```sh
nix develop -c python3 scripts/gateway/manage-workload-key.py \
  --endpoint http://10.144.44.70:4000 \
  --alias aginctus-approved-model \
  --output "$HOME/.local/state/aginctus/pilot-keys/workload.key" \
  --revoke
```

On an uncertain grant or revoke, **do not** delete the local file
manually, share it in chat, or assume the remote key is gone. Inspect
only safe metadata and use controlled revocation. Never print raw
LiteLLM HTTP responses or the gateway's expanded Incus configuration.

## Full offline validation

```sh
nix develop -c python3 -m unittest discover \
  -s scripts/gateway -p 'test_manage_workload_key.py' -v
nix develop -c python3 -m py_compile \
  scripts/gateway/manage-workload-key.py \
  scripts/gateway/test_manage_workload_key.py

just check
nix flake check
git diff --check origin/main...HEAD
git status --short

# Fully inert preview; no files or network need to exist.
nix develop -c python3 scripts/gateway/manage-workload-key.py \
  --endpoint http://10.144.44.70:4000 \
  --alias aginctus-approved-model \
  --output /does/not/exist/workload.key
```

Unit tests mock the *entire HTTP transport*, including issuance,
scope mismatch, revocation confirmation, endpoint restrictions, secret
files, and no-op preview. No live key issuance or OAuth is performed
by tests. Local trusted-host testing against pinned LiteLLM, a real
approved registered alias, and a revocation run remains mandatory
before closing #122, and must be explicitly authorized before claiming live #122 acceptance.
