# Native LiteLLM ChatGPT OAuth gateway decision (v0.1)

Status: accepted architecture, **integration not yet deployed**. Tracked in #187; supersedes unmerged #186 and the custom OpenAI inference daemon from #170. Keep LiteLLM as the sole workload-facing inference gateway and preserve v0.1's shared LiteLLM API key.

## Verified upstream capabilities and remaining uncertainties

Aginctus pins `ghcr.io/berriai/litellm:v1.103.0`. The upstream GitHub tag `v1.103.0` includes `litellm/llms/chatgpt/authenticator.py` and the ChatGPT provider package:
https://github.com/BerriAI/litellm/tree/v1.103.0/litellm/llms/chatgpt

LiteLLM's ChatGPT subscription provider documentation:
https://docs.litellm.ai/docs/providers/chatgpt/
documents `chatgpt/` models, `model_info.mode: responses`, native Responses routing and persisted authentication configured by `CHATGPT_TOKEN_DIR` and `CHATGPT_AUTH_FILE`. This describes the provider; **do not assume every current documentation detail is available in the pinned tag**. Check streaming events, tool-call events, statuses, supported model names, and credential-refresh behavior on the pinned image, then pin/update to a verified compatible release if necessary.

## Ownership and topology

```text
OpenCode / Hermes -> LiteLLM (private endpoint, v0.1 shared key)
                              |
                              +--> conventional API-key providers
                              |
                              +--> native chatgpt/ Responses provider
                                      |
                                      +-- managed credential volume
                                      |   (LiteLLM only)
                                      +--> ChatGPT subscription backend
```

The managed volume `aginctus-gateway-<id>-credentials` belongs to the **LiteLLM process acting as the credential manager**. Never attach it to PostgreSQL or workloads. A writable mount is needed because LiteLLM refreshes tokens. A successful Incus volume creation alone says nothing about secure mounts. Existing daemon images may remain available for development but must not be deployed for OAuth inference.

After authenticated host handoff, LiteLLM is the sole refresh-token owner; host refresh must be disabled or rejected. Require explicit user authorization and account selection at handoff. Treat ambiguous acknowledgement/crash as recovery-required and fail closed. Never put OAuth tokens in generated Compose/Jsonnet, Nix store, process arguments, Incus instance configuration, or logs.

It is **not enough** to mount arbitrary host authentication files or copy tokens into the volume. Establish a tested, permission-constrained, atomic import/refresh protocol or choose a documented LiteLLM-native device-flow enrollment process with exclusive ownership. The compatibility of the existing Aginctus host OAuth credential representation with LiteLLM's format is unverified.

## v0.1 implementation checkpoints

1. Add opt-in native ChatGPT Responses route to LiteLLM config; preserve current key providers and default behavior.
2. Securely attach only the managed volume to LiteLLM with writable ownership and restart persistence. Validate Incus project/pool/device placement; no volume in PostgreSQL or workloads.
3. Implement authenticated device-flow enrollment/handoff, single refresh owner, account binding, logout/revocation, and recovery rules.
4. Prove pinned-version Responses streaming, tool-call events and completion statuses with actual OpenCode traffic through LiteLLM.
5. Verify that the `aginctus-egress` bridge is present or reconcile it through the existing gateway provisioning path before attempting live deployment. The earlier #186 daemon deployment failed because the bridge was missing, not because provider credentials were invalid.


## Local model alias registration (inert, pre-activation)

Aginctus can now persist a workload-facing alias intent **without** activating a LiteLLM deployment, starting OAuth, selecting an upstream account, or asserting that a model name is available:

```sh
aginctus gateway provider chatgpt register --alias=personal
aginctus gateway model coding register --provider=personal --source=chatgpt/OPERATOR_VERIFIED_MODEL
aginctus gateway model list
aginctus gateway model coding show
```

Replace `OPERATOR_VERIFIED_MODEL` only after independently checking account entitlements and the pinned provider's supported model identifiers. `gateway model ... register --dry-run` still previews only the nonsecret LiteLLM Responses model fragment and does not persist state. Bare `chatgpt/MODEL` registration defaults to the `chatgpt` provider identity, which must first be registered.

The model registry is `${XDG_STATE_HOME:-$HOME/.local/state}/aginctus/gateway/models.json` (0700 parent, 0600 file), separate from `providers.json`. It validates aliases, requires a previously registered ChatGPT provider alias, serializes writes, rejects suspicious files and duplicate conflicting registrations, and provides read-only inspection. The records are nonsecret **intent**, not deployed routes; downstream #123 must receive only a separately authorized workload-facing alias and LiteLLM key. #124 must verify the alias against a *real terminal Responses event*, not registration success. This stage does not add a new public listener or modify gateway networks.

### Activation blockers verified against pinned LiteLLM v1.103.0

The upstream `litellm/llms/chatgpt/authenticator.py` implementation directly rewrites `auth.json` via `open(..., "w")`, does not implement interprocess locking or atomic token replacement, and can trigger device-code login as a side effect of model resolution. It also embeds some upstream error details in exception messages. Consequently **do not enable `CHATGPT_TOKEN_DIR`, configure an active `chatgpt/` route, or run live OAuth yet**, even after #217 prepares a private file. A later, explicitly reviewed activation must enforce one refresh owner, safe persistence, token-safe errors, and a controlled authorization boundary, ideally through a confirmed compatible upstream release rather than a duplicate OAuth implementation.

OpenAI's individual Terms of Use (https://openai.com/policies/terms-of-use/) restrict automated/programmatic output extraction. The applicability to routing a personal ChatGPT subscription through LiteLLM for workload inference is not established here; obtain a documented authorization basis before enabling this route. Use an approved API-key-backed provider as a separate supported route when appropriate.

## Scope guardrails

Do not add another OpenAI Responses proxy or multiple LiteLLM instances by default. Use only supported ChatGPT models and account permissions. Explicitly error on unsupported features. Per-workload LiteLLM identities belong in v0.2 (#177), not this milestone. No live OAuth credential deployment occurs as a side effect of merging this document.
