# LiteLLM OCI egress: pilot migration and acceptance (#257)

This is an **operator-controlled maintenance procedure**, not an automatic
deployment step. Never run it from CI or during ordinary `aginctus gateway
apply`. The Incus CLI must target the intended **local** daemon, not a remote.

## Preconditions (all required)

- The approved change window includes an interruption of **LiteLLM only**;
  PostgreSQL must stay running. Obtain an operator-approved recovery plan.
- Confirm the Incus *server* supports OCI NIC `ipv4.address` with a CIDR
  and `ipv4.gateway=none`. The pilot uses Incus 7.5.1; check again.
- Resolve the bridge's **current** subnet and gateway from metadata; do not
  copy observed addresses from prior sessions. The egress bridge must be owned
  by Aginctus, NAT and routing enabled, and IPv6 disabled. The management
  bridge must remain non-NAT and non-routed.
- Choose an unused static **host CIDR** in the bridge subnet, outside the
  DHCP pool, bridge gateway, network and broadcast addresses. This choice
  requires a contemporaneous **all-project** device inventory and lease
  inspection. A snapshot is not a durable reservation. Restrict the DHCP
  pool before assigning the NIC, and keep that restriction in declarative
  Aginctus configuration.
- Use a nonoverlapping, operator-reviewed range that retains **every
  existing DHCP lease**. Verify other clients' fixed assignments as well.
  Restricting DHCP can affect future renewals; do not treat a successful
  preflight as a guarantee against concurrent allocation.
- Set these Aginctus keys to reviewed values in the effective operator
  configuration (not on a command line or in chat):
  `gateway.egress.network.ipv4.address` (bridge CIDR),
  `gateway.egress.network.ipv4.dhcp.ranges` (restricted pool), and
  `gateway.egress.ipv4.address` (LiteLLM host CIDR).
  Review the rendered gateway plan for drift and consistency.

## Phase A: read-only preparation

```sh
incus version
incus network show aginctus-egress
incus network list-leases aginctus-egress
# Check named instance state, NOT expanded config (contains secrets):
incus list --project aginctus-gateway
aginctus gateway egress dhcp plan --ranges='<APPROVED_START>-<APPROVED_END>'
aginctus gateway egress reconcile --dry-run
```

A dry-run may reject the still-unrestricted DHCP pool until phase B.
Independently review egress consumers in **every Incus project**. Abort if
an inventory query fails, an IP is ambiguous, unrelated instances use
the proposed static address, or a concurrent operator is changing networking.

Do **not** paste `incus config show --expanded`, raw expanded API objects,
`env`, authentication headers, gateway keys, or database URLs into
issues, logs, or chat.

## Phase B: explicitly authorized bridge migration

Only after human review of the *actual* live leases, address and ranges:

```sh
aginctus gateway egress dhcp apply \
  --ranges='<APPROVED_START>-<APPROVED_END>' \
  --confirm-bridge-only
incus network show aginctus-egress
incus network list-leases aginctus-egress
aginctus gateway egress reconcile --dry-run
```

Verify the restricted pool is persisted and does not include the selected
static address. ETag protects against concurrent **network configuration**
writes, not a transaction involving DHCP leases and instance NICs.
Stop if any lease or ownership check changes unexpectedly.

## Phase C: LiteLLM-only stopped-instance reconciliation

This phase requires a planned LiteLLM interruption and a restorable
record of its **NIC settings only**. Do not stop PostgreSQL, recreate
instances, detach volumes, or modify secret-bearing instance config.

```sh
# Coordinate with workload users, then stop ONLY LiteLLM:
incus stop litellm-1 --project aginctus-gateway
# Repeat the collision and topology audit while stopped:
aginctus gateway egress reconcile --dry-run
# Only after the approved review:
aginctus gateway egress reconcile --confirm-stopped-instance
# Inspect NIC keys only; do not dump expanded instance config:
incus config device show litellm-1 --project aginctus-gateway
```

The reconciliation command requires the instance to be stopped, performs an
ETag-conditioned instance update, and verifies persisted NIC declarations.
It does **not** start the instance. If update verification fails, do not
start it; investigate current state and restore the reviewed prechange
NIC settings using an operator-approved recovery procedure. No automatic
rollback is promised.

Once NIC declarations are confirmed, the operator may start **only**
LiteLLM and independently verify service readiness. Do not proceed to
model-backed inference until guest networking passes phase D.

## Phase D: guest and end-to-end acceptance

1. Inspect the actual OCI guest address and routing state via tools present
   in the image (for minimal images, `/proc/net/route` and interface
   metadata are more reliable than assuming `ip` is installed).
   Confirm both management and egress IPv4 addresses; the **only default
   route** must use the egress bridge, never management.
2. Confirm management DNS/PostgreSQL reachability remains operational and
   that the egress route reaches the expected HTTPS provider endpoint.
   Inspect the certificate-validated TLS result; do not weaken
   verification or copy upstream credentials.
3. Prove PostgreSQL and representative management-only workloads have
   **no** direct upstream route. Leave management bridge NAT and routing
   disabled; inspect actual host firewall isolation as appropriate.
4. Verify LiteLLM readiness, then perform exactly one operator-approved,
   low-cost authenticated `/v1/responses` request through the gateway.
   Redact authorization headers and model outputs before recording proof.
5. Audit persistence: existing gateway virtual key/model registration,
   PostgreSQL storage and private credential volume remain intact.
6. Report pass/fail for each acceptance criterion on #257. Keep #257 open
   until real provider inference succeeds and management isolation holds.

## Failure/recovery gates

- **Before any write:** fail closed; repair configuration or inventory.
- **After bridge update but before NIC update:** leave LiteLLM unchanged;
  recheck bridge ranges, leases and any affected consumers. Restore the
  original DHCP config only with a separately reviewed change; blindly
  widening a live pool may introduce collisions.
- **After NIC update but before start:** keep LiteLLM stopped if its
  persisted settings differ or cannot be inspected. Restore only the
  recorded NIC keys, without writing credentials, databases or volumes.
- **After start:** if management/database or egress is broken, stop
  LiteLLM, inspect/redact NIC state and routes, and use the reviewed
  NIC restoration plan. Never destroy/recreate PostgreSQL or LiteLLM.
- **No retries on uncertain completion:** an Incus operation may have
  reached the server despite a timeout. Inspect persisted state first.

## Evidence to attach to #257

Incus server version; redacted network metadata and lease inventory;
the chosen CIDR and range with reservation rationale; dry-run results;
only the affected NIC keys; redacted guest route and interface evidence;
TLS/Responses success status; and management-only negative-control results.
