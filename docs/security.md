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
  file permissions, token refresh, account authorization, or OAuth safety.
  [Native ChatGPT OAuth](architecture/native-chatgpt-oauth.md) remains gated;
  neither volume attachment nor model registration activates verified inference.

## Validation boundary and remaining acceptance

Hermetic tests exercise parsing, rendering, ownership failures, and synthetic
protocol behavior. Disposable VM tests exercise host firewall integration;
recorded offline real-Incus checks exercise guest credential delivery. These
do not establish the pilot's deployed firewall, guest routes, effective key
scope, provider TLS, or successful model-backed tool execution.

[Release #118](https://github.com/blevinn/aginctus/issues/118) is the acceptance
checklist: [#257](https://github.com/blevinn/aginctus/issues/257) requires live
gateway-only egress and isolation evidence; [#122](https://github.com/blevinn/aginctus/issues/122)
requires authenticated upstream inference and is on operator hold pending that
evidence; [#123](https://github.com/blevinn/aginctus/issues/123) requires real
managed-workload inference; [#124](https://github.com/blevinn/aginctus/issues/124)
requires the repeatable coding task and cleanup. The bundled
[live acceptance dispatcher](acceptance-opencode-live.md) requires reviewed
operator hooks; mocks, no-op hooks, and health HTTP 200 are not live acceptance.

v0.1 uses a shared management network, not workload-to-workload segmentation.
Trust the host, Incus administrators, and reviewed images; ownership labels
are not protection against an administrator. Gateway HTTP carries bearer keys
in plaintext inside the private bridge; see the [API-key pilot](gateway-api-key-pilot.md).
Do not publish expanded Incus configuration, which can contain gateway secrets
and database URLs. Live migration, credential use, and billable inference
require separate operator authorization under the linked runbooks and gates.
