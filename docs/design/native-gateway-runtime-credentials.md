# Native gateway runtime credentials: authorization and delivery contract

Status: implementation gate for #158, #171 and #698. No production activation is approved by this document.

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

### Source review: service consumption and image provenance

- The Docker Official Images entry for `17-alpine` identifies a build from the PostgreSQL image repository, not a fixed deployed digest. The reviewed [17 Alpine build](https://github.com/docker-library/postgres/blob/ac9627dcf544a8454f212a12ddeebfcdc0803d3e/17/alpine3.24/Dockerfile) copies the [entrypoint](https://github.com/docker-library/postgres/blob/ac9627dcf544a8454f212a12ddeebfcdc0803d3e/docker-entrypoint.sh). It reads `POSTGRES_PASSWORD_FILE`, rejects setting both the file and value, and converts file bytes into process-local state. When run as root, it re-enters as the image's Alpine `postgres` UID/GID 70; file ownership and directory traversal must support the exact approved runtime identity, without granting access to other guests. When `PG_VERSION` already exists, initialization is skipped; replacing that file does **not** change the login password in PostgreSQL. The startup query explicitly requires libpq's [SCRAM handshake](https://www.postgresql.org/docs/17/libpq-connect.html#LIBPQ-CONNECT-REQUIRE-AUTH) over loopback using a separate fixed-path guest `PGPASSFILE`; a successful query against `trust` is not readiness.
- The LiteLLM `v1.103.0` source tag resolved to `c991f4b01f5799eb0b0cab0fb63988e15c3a8a9d`. Its [Dockerfile](https://github.com/BerriAI/litellm/blob/c991f4b01f5799eb0b0cab0fb63988e15c3a8a9d/Dockerfile) uses `/app/.venv` and a [production entrypoint](https://github.com/BerriAI/litellm/blob/c991f4b01f5799eb0b0cab0fb63988e15c3a8a9d/docker/prod_entrypoint.sh) that invokes the CLI without a `_FILE` adapter. The [CLI](https://github.com/BerriAI/litellm/blob/c991f4b01f5799eb0b0cab0fb63988e15c3a8a9d/litellm/proxy/proxy_cli.py) sets/uses `DATABASE_URL` for Prisma and has a debug log of the input URL. The [secret helper](https://github.com/BerriAI/litellm/blob/c991f4b01f5799eb0b0cab0fb63988e15c3a8a9d/litellm/secret_managers/main.py) recognizes explicit OIDC file references; that branch does not make arbitrary `*_FILE` settings work for database, master and salt initialization. Do not assume a secret-bearing config file is safe either: verify handling through config reads, logs and persistence.
- In that LiteLLM source, [the health router](https://github.com/BerriAI/litellm/blob/c991f4b01f5799eb0b0cab0fb63988e15c3a8a9d/litellm/proxy/health_endpoints/_health_endpoints.py) authenticates `/health/readiness/details`. The probe requires unauthenticated denial, authenticated success, and `db: connected`; it sends the master key only from within the guest to its loopback socket. `/health/readiness` is public; `/health` can call configured providers. The reusable probes are single checks; an approved composer still needs a bounded readiness wait that distinguishes service startup from denial and rechecks pin, credential version and lease throughout.
- This is source inspection, not authenticated source-to-image provenance. Before using real images, resolve each approved repository to the exact platform manifest/config digests, verify source/build binding independently of arbitrary image labels, inspect the actual entrypoint and copy of the script in that image and verify Incus runs the same UID, mounts and working directory. A layer fingerprint alone cannot identify the OCI config. The disposable synthetic process fixture does not validate any of these upstream claims.

### G1: bounded guest consumption candidate

- **PostgreSQL:** retain the upstream entrypoint; use only the nonsecret `POSTGRES_PASSWORD_FILE=/run/aginctus/gateway/postgres-password` reference, `POSTGRES_USER=litellm`, `POSTGRES_DB=litellm`, and `POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256` for fresh initialization. The guest handoff must permit UID/GID 70 to traverse and read its private file on both entrypoint invocations. Keep the independent loopback `postgres.pgpass` for the authenticated readiness query. The existing disposable OCI fixture exercises upstream file initialization, retained data and role-password rotation with fixture-only paths under `/var/lib/postgresql`; it does not validate the proposed `/run` mount or authorize reuse of its admin file API handoff.
- **LiteLLM:** the [guest bootstrap](../../internal/gateway/guest/litellm_bootstrap.py) is an unwired script, not an installed command. A future trusted launcher must pin the exact script and image bytes, `/app` working tree, guest identity and invocation (`/app/.venv/bin/python -I -S`, fixed script, canonical PostgreSQL guest name derived from the same reviewed intent). The syntactic name check does not bind the name to a project, prove DNS or authenticate the DB endpoint. Prove that the name reaches only the intended PostgreSQL guest over an independently approved management path before handing off a password. The bootstrap has no network, writes no credential files and takes no configurable paths or options.
- Under the guest's `/run/aginctus/gateway`, LiteLLM reads only `postgres-password`, `master-key` and `salt-key`, traversing protected root-owned ancestors without symlinks. The final directory is mode `0700` and owned by the exact service UID; files must be single-link regular files with that owner and mode `0400` or `0600`. The candidate wire format is 1–255 ASCII URL-unreserved token characters from `[A-Za-z0-9_-]`, with at most one trailing LF. The publisher must independently enforce strength, consistent versions and immutable handoff during startup; file shape proves none of these. The `master-key` path and format match the existing readiness probe; that probe must also run with the approved guest identity.
- The bootstrap constructs `DATABASE_URL` inside the guest and replaces itself with one fixed LiteLLM CLI invocation, using only the three secret environment entries and fixed nonsecret runtime settings. No optional config, tracing wrapper, inherited child environment or dotenv input is used. Standard I/O is discarded before exec because even upstream errors may contain secrets; a generic nonzero status and independent redacted readiness are the bounded diagnostics. Process environments and descendants (including Prisma) remain sensitive. Before deployment, verify guest `/proc`, ptrace, crash collection, process/exec inspection, child environments, on-disk artifacts, API responses and non-stdio sinks. A core size limit or stderr suppression does not prove that isolation.
- The hermetic test exercises the exact bootstrap with synthetic files and a stand-in at the exec boundary. It covers hostile file shapes, missing inputs, argument overrides, ambient environment exclusion, suppressed output and a failed exec. It does not run LiteLLM, Prisma, an OCI guest or Incus. Next, on a verified dedicated disposable worker, pin and inspect the actual images and bootstrap, then test both real consumers with only synthetic credentials, authenticated DB and LiteLLM readiness, wrong credentials, logs/metadata and principal-denial checks. No production publisher, launcher, credential receipt or activation grant is added; accepted gateway reconciliation still stops at stopped provisioning.

### Rotation and recovery gate

Treat a database password change as a separate authorized state machine. Quiesce LiteLLM under its own stop grant, verify existing PostgreSQL identity and old credential, then apply an authorized PostgreSQL role-password change against the retained data. Stage the new guest material under a new opaque version and verify new authentication, old-version denial, and a pre-rotation data marker before publishing readiness. On interruption after the database write, the login state may have changed even if delivery did not; require redacted observation-based recovery rather than deleting data, repeating initialization, or blindly reverting. Keep database-password rotation separate from LiteLLM master/salt changes. Prove virtual-key validity/revocation and retention of the application's encrypted provider values and OAuth state before authorizing a master/salt change; do not assume changing a key in files re-encrypts stored values. LiteLLM remains the sole OAuth refresh credential owner under ADR 0004.

The versioned binding is only infrastructure metadata: it supplies the canonical environment revision envelope, explicit service image identities and storage pools to the independently trusted verifier. No authority is established by constructing it. The existing canonical environment candidate is workload-only; accepting that candidate alone does not approve these gateway inputs. An independently owned record or future reviewed accepted schema must bind the exact revision to the complete gateway intent, credential version and trusted source. Admissible handoff must prove that project metadata access does not imply guest exec, guest files, volume files, snapshot/export or other read paths to credential material. A restricted project is not itself this proof.

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

### Protected placement implementation increment

The #1167 approved protected placement is independently selected by an
administrator-owned `aginctus.dev/protected-gateway-grant/v1alpha1` JSON file
with `serverId`, `project` (logical), `environmentId`, `revisionId`, and
`protectedProject`. The exact accepted revision commits to the complete
image, network, pool and credential-version intent. The candidate cannot
select the physical project. The first implementation uses a dedicated
nondefault gateway project on the same pinned standalone daemon, marked
`user.aginctus.logical-project`, `user.aginctus.protected-purpose=gateway`
and the canonical environment ID. Keep it separate from state storage.

The installed administrator-only `aginctus internal gateway-protected-preview GRANT OPERATOR_CONFIG`
role takes the grant path and administrator-held pinned operator configuration
as its two positional inputs (see usage on an invalid invocation). It reads the
actual accepted snapshot over mTLS, verifies the local pinned daemon and both
project policies, requires exact network/pool allowlists, and observes the
physical pair and retained database. This is a read-only prerequisite: the
report explicitly does not hold a writer pin, establish OCI acquisition
provenance or permit runtime activation. No gateway credentials are read.
