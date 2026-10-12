# Aginctus OAuth gateway runtime and credential handoff design

Status: **proposed, not implemented**. Tracks #171 and is layered after PR #175 (storage) and PR #170 (host OAuth). The Responses adapter is #172. This document deliberately does **not** authorize token import or any new mounted service.

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
4. **OpenCode**: sees a private Aginctus inference endpoint and an
   Aginctus-scoped credential, **never an OpenAI OAuth bearer or refresh token**.
5. **LiteLLM/PostgreSQL**: remain an optional API-key provider path; no direct
   access to SIWC state.

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
  settings. Keep the daemon distinct from LiteLLM/PostgreSQL.
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
7. Do not claim end-to-end readiness until #172 tests an authorized
   account's streamed Responses completion and OpenCode behavior.

## Sequencing / merge strategy

- **PR #170**: host login, local lifecycle and account records (draft).
- **PR #175**: owned **unmounted** Incus volume (draft; this is its
  immediate dependency).
- **This design PR**: freeze the ownership protocol and runtime boundary.
- **Runtime follow-up**: build a dedicated daemon image and mount, then
  secure handoff, ownership tests, and Incus smoke testing.
- **#172**: OAuth-specific Responses adapter and restricted OpenCode routing.

Do not merge the design alone as completion of #171 and do not instruct users
to place OAuth tokens in the Incus volume manually.
