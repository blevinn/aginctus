# Native gateway runtime credentials: authorization and delivery contract

Status: implementation gate for #158, #698. No production activation is approved by this document.

## Problem and current implementation

The legacy Compose gateway passes the PostgreSQL password, PostgreSQL URL, LiteLLM master key and LiteLLM salt as environment values. The native OCI gateway can create both service containers **stopped**, and read-only startup inspection (#840/#842) checks for those names. **Present environment keys do not establish secure provisioning, provenance, credential correctness, or permission to start**.

The native credential-exposure audit distinguishes legacy config exposure from missing evidence. It cannot authorize startup.

## Selected security properties (required)

1. **Accepted revision and principal:** A root/admin authority must select an immutable accepted deployment revision, with explicit project, two instance identities, exact OCI manifest/config identities, storage attachment and credential version. An operator-mode candidate must not cause host writes by supplying its own credential path, arbitrary root volume, image or network. Validate the #698/#704 principal boundary before any privileged execution.
2. **Secret provenance:** Credential bytes originate from an administrator-controlled, root-owned store or an independently authorized secret service, never directly from ambient environment or unreviewed user configuration. The executor must validate ownership, symlink resistance and restrictive permissions on every read; no log, plan, PR comment, error, telemetry or acceptance artifact may contain values.
3. **Runtime handoff:** Credential delivery must reach only the intended guest(s) in the selected project. Secret-bearing volumes must be independently owned, have exact mount destinations, and reject all unexpected `UsedBy` instances/profiles. Never place long-lived secret bytes in `environment.*` Incus instance config. Where an upstream image requires environment values, establish and test a narrowly scoped, in-guest consumption mechanism that avoids persistent Incus config disclosure **before** implementing it.
4. **Database contract:** PostgreSQL must initialize and authenticate as the expected `litellm` user against the persistent database. The LiteLLM database URL is sensitive because it embeds the password; a DB URL in Incus config is not a secure substitute. Changing the password must preserve existing PostgreSQL data and support coordinated rotation rather than silently reinitializing the database.
5. **No unsanctioned host capabilities:** Guest instances must not acquire privileged mode, arbitrary host filesystem disks, raw LXC configuration or broader network/storage access during injection or repair. Reject foreign/unowned resources before and at the mutation boundary, with a daemon-wide writer fence as applicable.
6. **Startup authority:** Even a verified receipt from delivery must not itself confer a start grant. Reconfirm pinned accepted revision, current credential version, project/instance ownership, image identity, device attachments and executor lease immediately before each state transition. Start PostgreSQL, then wait for authenticated PostgreSQL readiness, then start LiteLLM and verify an authenticated, provider-independent health path.
7. **Failure and recovery:** Report stage, attempted writes, uncertain results and verified partial progress *without credential contents*. Stop further work after failures, retain the PostgreSQL volume, avoid implicit deletion/replacement or blind retry. A stale lease, revoked revision or rotated credential must block further operations, including after PostgreSQL starts.
8. **No-credential evidence in audit:** Redacted API outputs must contain only names, revision identifiers and non-sensitive statuses. Negative tests must prove that a caller with read-only Incus project access cannot recover credential bytes from image/instance/volume metadata, API observations, or CLI-readable configuration.

## Candidate delivery approaches requiring explicit review

- **Guest-owned secret file handoff** via a strictly owned, project-local volume and in-guest read with restrictive mode: investigate actual Postgres `*_FILE` support and LiteLLM bootstrap behavior. Volume isolation and read access need real Incus tests.
- **One-time in-guest injection** through a narrowly scoped agent/provisioner: consider exposure in exec arguments, process environment, logs, daemon operations and retry artifacts; require bounded lifetime and real tests.
- **Admin-controlled secret broker** with separate, scoped grant: requires evidence of authenticator identity, revocation and no escalation across the NixOS/Home Manager principal boundary.

These are candidates, **not implemented or approved architectures**. Do not choose by simply reusing legacy Compose env expansion or exposing host secret paths.

## Disposable Incus acceptance required before startup PR is merge-ready

- Positive authorized credential provisioning and consumption without persistent Incus environment values.
- Wrong project, wrong guest, foreign volume and duplicate mount denial; host-disk/raw-config denial with state invariants.
- Nonadministrator project operator cannot inspect credentials or invoke a privileged host-write proxy.
- Authenticated database readiness and LiteLLM health with only synthetic credentials; fail closed on missing/revoked/wrong versions.
- Repeated start no-op, cancellation between the two services, rotation with data preservation, and redacted partial-failure artifacts.
- Confirm explicit teardown retains database contents; document rollback from existing Compose resources under #202.

## Implementation sequencing

1. Preserve the redacted runtime exposure and startup inspection as **observers**, not grants.
2. Review and select a delivery approach using the #698 authority decisions; add a version-bound credential receipt backed by independent verification.
3. Implement isolated credential delivery and real-Incus negative tests before a production-exposed startup entry point.
4. Wire ordered, guarded SDK startup plus readiness, then parity, migration and legacy-driver retirement.

Related: #153, #156, #158, #159, #202, #698, #704.
