# Security controls and limitations

Aginctus is a single-operator lab in v0.1 hardening. This is a summary of
implemented controls and remaining acceptance, not a completed threat model,
security audit, or production-readiness claim.

## Implemented controls

- **Network policy:** management bridge verification requires IPv4 NAT and
  routing disabled and IPv6 disabled. Host DNS/DHCP exceptions derive from
  verified Incus ownership, bridge identity, and addressing, not a name prefix.
  See [host firewall policy and backend limitations](firewall-host-backends.md).
  New management bridges explicitly suppress DHCP default gateway announcements
  while retaining DHCP addressing and DNS. Existing bridges require the
  [separately approved DHCP migration](management-dhcp-gateway.md); absence of a
  guest default route is not proof of packet enforcement.
  LiteLLM has a separate egress attachment; PostgreSQL and workloads remain
  management-only in the intended topology. The
  [egress migration and acceptance procedure](litellm-oci-egress-pilot.md)
  defines the required live route, TLS, and no-bypass checks.
- **Resource ownership and isolation:** workload lifecycle checks ownership
  markers by default; explicit `--force` can bypass workload ownership checks.
  Containers share the host kernel. VM isolation requires an operator-supplied
  compatible image; the default OpenCode image is a container. See
  [workload ownership and image constraints](workloads/lifecycle.md).
- **Workload credentials:** the OpenCode launcher requires valid runtime gateway
  configuration and a private regular workload-key file before starting. It
  supplies the key at runtime, not through the Nix store. This configures the
  client, not an independent network boundary. See the
  [workload key lifecycle](gateway-workload-key-pilot.md) for scoped issuance,
  revocation, and outstanding runtime checks.
- **Gateway credential storage:** attachment checks enforce owned, project-local
  storage exclusive to LiteLLM. The metadata-only audit does not verify guest
  file permissions or runtime ownership. The SIWC implementation's enrollment,
  atomic token state and in-process refresh tests are separate from deployment
  acceptance. [SIWC plan usage](gateway-siwc.md) requires deliberate secure
  handoff and gateway activation; live provider permission, deployed guest
  ownership, real inference/tool execution and workload isolation remain
  unverified. The native Codex-device `chatgpt/` provider remains inactive.
- **SIWC contract option:** [SIWC plan usage](gateway-siwc.md) separates issued
  client/account registrations, binds entitled models to explicitly consented
  Aginctus workload-key identities, and refreshes only inside the LiteLLM owner.
  Private atomic state, a process lease and refresh journal prevent concurrent
  rotation/replay after ambiguous failure. Enrollment uses verified issuer keys,
  state/nonce/PKCE and a loopback listener. Handoff disarms the enrollment host
  before publishing a private, destination-bound capsule. Capsule transfer,
  runtime UID/mount checks and gateway activation remain trusted operator work;
  no root/Incus-administrator isolation or live provider acceptance is claimed.

## Validation boundary and remaining acceptance

Hermetic tests exercise parsing, rendering, ownership failures, and synthetic
protocol behavior. Disposable VM tests exercise host firewall integration;
recorded offline real-Incus checks exercise guest credential delivery. These
do not establish the pilot's deployed firewall, guest routes, effective key
scope, provider TLS, or successful model-backed tool execution.

[Release #118](https://github.com/blevinn/aginctus/issues/118) records accepted
live pilot coding/inference at `57c2da6`, scoped credential cleanup and the
October 29 runtime/permanent network audit and targeted packet controls.
These are independent observations, not conclusions drawn from hermetic tests.
[#122](https://github.com/blevinn/aginctus/issues/122) and
[#123](https://github.com/blevinn/aginctus/issues/123) retain explicit
recovery, attribution and evidence-tier decisions. Full fresh-host execution
of the [deployment/acceptance procedure](v0.1-deployment-acceptance.md) is pending.
Neither no-route nor a timeout proves routed IPv6 enforcement or exact packet
verdicts; disruptive live recovery remains unrun. The bundled
[live acceptance dispatcher](acceptance-opencode-live.md) requires reviewed
operator hooks; mocks, no-op hooks, and health HTTP 200 are not live acceptance.

v0.1 uses a shared management network, not workload-to-workload segmentation.
Trust the host, Incus administrators, and reviewed images; ownership labels
are not protection against an administrator. Gateway HTTP carries bearer keys
in plaintext inside the private bridge; see the [API-key pilot](gateway-api-key-pilot.md).
Do not publish expanded Incus configuration, which can contain gateway secrets
and database URLs. Live migration, credential use, and billable inference
require separate operator authorization under the linked runbooks and gates.
