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

## Scope guardrails

Do not add another OpenAI Responses proxy or multiple LiteLLM instances by default. Use only supported ChatGPT models and account permissions. Explicitly error on unsupported features. Per-workload LiteLLM identities belong in v0.2 (#177), not this milestone. No live OAuth credential deployment occurs as a side effect of merging this document.
