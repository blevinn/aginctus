# Aginctus OAuth gateway runtime and credential handoff design

Status: **proposed, not implemented**. Tracks #171 and is layered after PR #175 (storage) and PR #170 (host OAuth). The Responses adapter is #172. LiteLLM is the **mandatory workload-facing inference ingress** in both API-key and OpenAI/OAuth modes. v0.1 may share a static workload gateway key; dedicated workload identities and fine-grained authorization are deferred to v0.2. This document deliberately does **not** authorize token import or any new mounted service.

## Problem and trust boundaries

A successful browser OAuth login belongs to the **host user**. The gateway must
not consume those credentials merely because a host CLI can read them.
Conversely, a gateway that owns a rotating refresh token must be the **only
actor** allowed to refresh that token. Two independent refreshers can invalidate
a rotated token permanently.

Today: the host writes account-scoped files in
`$XDG_STATE_HOME/aginctus/providers/openai/accounts/`; #175 provisions
`aginctus-gateway-<id>-oauth` as an owned but **empty and unmounted**
custom Incus volume. LiteLLM and PostgreSQL must not be given this volume.

## Component boundaries

1. **Host OAuth CLI**: interactive authorization, selection, host account
   identity and public metadata, and explicit requests to provision/revoke
   the gateway's access. Prior to handoff, only this CLI may refresh tokens.
2. **OAuth gateway daemon** (new, minimal Go executable or dedicated
   `aginctus` subcommand): runs as an unprivileged user in its **own**
   NixOS/Incus image; holds an exclusive application-level file lock on its
   state, refreshes its selected account token, and serves an authenticated,
   restricted private inference interface to the later Responses adapter.
   It must have **only** the gateway's egress NIC and required private
   management NIC (or a narrower network if supported). The daemon must
   not be installed in the generic OpenCode workload.
3. **OAuth credential volume**: mounted **only** in that daemon container,
   e.g. `/var/lib/aginctus/oauth`, not in LiteLLM/PostgreSQL/Herdr/OpenCode.
   Mount by explicit custom-volume name from the correct storage pool with
   strict existing ownership markers, not a host bind mount. Configure
   explicit restrictive file/directory ownership and reject symlinks.
4. **OpenCode / Hermes**: connect **only to LiteLLM's private inference endpoint** using a static Aginctus-managed LiteLLM credential in v0.1 (a shared key is acceptable). They never receive an OpenAI OAuth bearer or refresh token. Static keys are not proof of a distinct workload identity, and this deployment must not claim per-workload isolation.
5. **LiteLLM/PostgreSQL**: LiteLLM is the **single mandatory workload ingress** for both API-key and OAuth inference. It checks its v0.1 gateway key and routes a configured model alias to the internal OAuth Responses adapter. LiteLLM itself must not receive the SIWC access or refresh tokens; PostgreSQL remains confined to LiteLLM's own persistence.
6. **Adapter ingress**: accept inference only from the LiteLLM service over a separate private connection with independently provisioned service-to-service authentication; never infer trust from a client-provided account header or solely from network reachability. This internal credential is **not** the static workload key. Enforce explicit configured consent/account selection server-side and reject any direct workload requests.

## Workload authentication scope and routing

The public *within Aginctus* model path for v0.1 is:

```text
OpenCode/Hermes
  │ private gateway URL + static LiteLLM key
  ▼
LiteLLM (sole workload ingress and stable client API)
  ├── standard provider adapter → external API-key provider
  └── configured OAuth model route → internal authenticated Responses adapter
                                  → credential owner → OpenAI Responses API
```

A single static LiteLLM key may be shared by managed workloads in v0.1. Treat it as a basic access barrier only: possession does **not** establish workload identity or per-workload accountability. Do not build an elaborate Aginctus-issued per-workload key registry, principal mapping, rotation protocol, or workload-specific grants in v0.1. Those are v0.2 concerns. The managed secret must nevertheless not appear in generated Nix closures, published Compose/Jsonnet plans, or logs; distribute it using the existing protected runtime provisioning mechanism and enforce private networking.

The OAuth adapter must distinguish its caller from arbitrary workloads by a **separate internal service credential and private listener**. Restrict direct adapter access, and choose the specific consented OAuth account using trusted server-side configuration, not arbitrary `account_id`, headers, or model aliases supplied by the request. With static workload credentials, an authorization policy cannot truthfully attribute calls to individual workloads: expose only a consciously approved *shared v0.1 account scope* and document that all holders of the shared key may use it. If this scope is too broad for consent/policy, disable OAuth inference rather than suggest artificial isolation.

LiteLLM compatibility is a **required pre-implementation spike**: demonstrate that the pinned LiteLLM version can authenticate an incoming request and forward a streaming `/v1/responses` call to a configured internal backend while preserving required request fields, SSE events, tool calls and terminal completion. If ordinary model routing, custom providers, or passthrough cannot do so safely, implement a *LiteLLM plugin/custom-provider integration* or explicitly revise this design; do **not** quietly bypass LiteLLM or inject an OAuth bearer into a generic API-key provider.

A static key is acceptable for v0.1 deployment, **not** for the internal host-to-gateway refresh-token handoff. Its stronger ownership, identity and replay requirements below remain unchanged.

## Handoff state machine

Use a persistent, versioned **manifest** on the host and an identity-bound
gateway account record. Each identity is `(issuer, sub, issued_client_id,
host_id)`; never select by email alone.

A host record has explicit `owner` and `transfer_id` fields. Valid states:

- `host_active`: host contains the refresh token, and host refresh is allowed.
- `transfer_prepared`: host lock acquired; **host refresh disabled**,
  transfer ID chosen randomly, per-target gateway identity and account ID
  fixed; token still recoverable locally. The target must not serve inference.
- `gateway_accepted`: the daemon validated target identity and account
  bindings, committed the credential to its private volume using fsync +
  atomic rename, and returned an authenticated durable receipt for the
  exact transfer ID and credential generation. Gateway is the sole
  refresh owner. The host must prevent refresh even if local cleanup fails.
- `gateway_active`: host verified the receipt and retired its local
  refresh-token copy. Other local metadata and issued client ID remain.
- `recovery_required`: ambiguous acknowledgment or crash. **Neither side
  may independently reset the ownership to host-active**. Resolve by
  querying the daemon's authenticated status for the same transfer ID;
  if unreachable, explicitly fail closed until operator recovery.

No automatic rollbacks after `gateway_accepted`: a remote daemon might
already be rotating the token. Failed preparation before acceptance may
roll back with the host lock if the destination demonstrably did not accept.

Any existing single-session legacy file must be migrated and deleted before
preparing. A logout that merely deletes the host's copy is insufficient if
the gateway owns a refresh token: it must request gateway-side revocation,
observe a durable reply, and distinguish local removal from remotely confirmed
revocation.

## Transport and authorization

Do **not** pass tokens via CLI arguments, process environment, rendered
Compose/Jsonnet, Incus instance config, logs, or an unprotected mounted host
directory.

Preferred minimal v0.1 transfer:

- Bootstrap a **one-time, mutually authenticated host-to-daemon control
  channel** using local Incus privileged control mechanisms (e.g. a
  Unix-domain socket available to the authorized host through the Incus
  instance control plane). Avoid exposing it on `aginctus-mgmt` unless
  peer identity and replay protection are solved.
- Establish and pin the daemon's instance identity / Aginctus ownership
  markers **before sending** a token; reject foreign or recreated instances
  with mismatched identity. Lock the destination account by transfer ID.
- Send the credential over the authenticated channel once; daemon validates
  account, issuer, client ID and generation, then durably commits and returns
  an authenticated non-secret receipt. Repeated same-ID requests must be
  idempotent; different-ID replay must fail.
- Do not claim durability based solely on an Incus file write or HTTP 200;
  require the daemon's acknowledged durable state transition.

Exact Incus transport requires an integration spike: standard `incus file
push` alone cannot provide reliable one-owner acknowledgments, and composing
an untrusted service port into the management bridge is not acceptable.

## Gateway orchestration changes required

- Introduce a separate OAuth daemon image derivation and immutable deployment
  settings. Keep the daemon distinct from LiteLLM/PostgreSQL; it is an **internal LiteLLM backend**, not a competing workload gateway.
- Add a preflight volume Ensure step before its Compose service, with no
  credentials in the generated plan. The storage pool and gateway ID must
  match the provisioned volume. Do not rely on Compose silently adopting an
  existing differently marked volume.
- Mount the single named custom volume read/write **only** into the daemon,
  and verify ownership, mode, correct project and correct guest path.
- Keep the `aginctus-mgmt` bridge unrouted and without NAT. Only an explicitly
  configured gateway daemon receives provider egress, following the
  independent egress design from PR #160; preserve the existing Herdr/
  OpenCode isolation.
- Configure LiteLLM as the only workload-facing ingress, a single static gateway key for v0.1, explicit OAuth model routing, and separate authenticated LiteLLM-to-adapter transport; the adapter is not exposed to workloads. Verify streaming Responses compatibility before serving live requests.
- `gateway render`, `gateway validate`, `gateway status`, and aggregate
  `apply --render` must clearly distinguish **declared**, **provisioned**,
  **mounted**, **authenticated**, and **inference-ready** conditions.
  A created volume alone must never display `authenticated`.
- New gateway instances must not be started with writable OAuth volumes
  except after their own identity, permissions, and authenticated daemon
  bootstrap are verified.

## Verification and failure injection

1. Unit-test every legal and illegal state transition, token-generation
   comparison, duplicate transfer, mismatched account/daemon identity, and
   reauth while ownership is gateway-side.
2. Verify restrictive filesystem modes, owner IDs, symlink/race rejection,
   atomic writes, durable fsync semantics, lock ordering, and process-crash
   recovery at each transition. Never use real tokens in test fixtures.
3. In a real Incus test, inspect daemon devices and confirm no OAuth volume
   on PostgreSQL, LiteLLM, Herdr or OpenCode, and no token values in Incus
   instance config, `gateway render`, or orchestrator logs.
4. Crash the daemon after volume commit but before host acknowledgment;
   demonstrate that host refresh remains disabled and recovery is
   idempotent without consuming a stale rotating token.
5. Confirm reboot/container recreation retains credentials and permits
   a single authorized refresh; use a controlled mock OIDC/token endpoint.
6. Test local-only logout, remote revocation failures, gateway inaccessible,
   account switching, and later reconnection. Do not mask a remote failure.
7. Confirm ordinary workload access to the OAuth adapter is rejected even when the workload knows the v0.1 LiteLLM shared key; only LiteLLM's separate backend identity is accepted. Confirm no claims of per-workload attribution from shared keys.
8. Validate LiteLLM's authenticated Responses passthrough, SSE events, tool-call fidelity, and incomplete-stream handling against the pinned version. Do not claim end-to-end readiness until #172 tests an authorized account's streamed Responses completion and OpenCode behavior.

## Sequencing / merge strategy

- **PR #170**: host login, local lifecycle and account records (draft).
- **PR #175**: owned **unmounted** Incus volume (draft; this is its
  immediate dependency).
- **This design PR**: freeze the ownership protocol and runtime boundary.
- **Runtime follow-up**: build a dedicated daemon image and mount, then
  secure handoff, ownership tests, and Incus smoke testing.
- **#172**: OAuth-specific Responses adapter connected **behind LiteLLM**; gateway compatibility spike, static shared key in v0.1.
- **v0.2**: dedicated per-workload LiteLLM identities/keys, policy bindings, issuance and rotation.

Do not merge the design alone as completion of #171 and do not instruct users
to place OAuth tokens in the Incus volume manually.
