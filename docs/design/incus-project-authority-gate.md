# Incus project authorization: threat model and verification gate

Status: **Design spike; not authorization sign-off.** Tracking: [#704](https://github.com/blevinn/aginctus/issues/704), [#693](https://github.com/blevinn/aginctus/issues/693). Read with [ADR 0004](../adr/0004-three-layer-state.md) and [three-layer design](three-layer-state.md).

## Decision boundary

The core **configured → accepted → current** model is independent of how system and Home Manager operators are authorized. The currently proposed *single accepted revision with system/user scoped CAS* is **provisional**. A validation or merge bug must not be able to convert an untrusted user's candidate configuration into privileged host operations.

The safest direction to **test first** is **host-admin authority for host capabilities**, with an **Incus-restricted project principal for delegated operator resources** and no bidirectional privileged config merge. The final solution remains gated on the real Incus experiments below; this file is not approval to activate cross-principal writes.

## Current Aginctus boundary: documented findings

- `internal/incus/project_scope.go` validates a named non-`default` project, and its current target configuration isolates images, profiles and storage volumes while using `features.networks=false`. This is resource *namespacing*, not user authorization; the proposed project policy does **not** currently set `restricted=true`.
- `internal/orchestration/native/reconcile.go` uses `incus.ConnectIncusUnixWithContext(...)` and `UseProject(project)`. Its local connection and selected project checks support safe targeting and ownership validation, but they do **not** prove that an operator on the privileged Unix socket is confined to that project. Do not repurpose that helper for an untrusted Home Manager writer.
- The NixOS `services.aginctus` module currently provides root-owned host firewall services and optional Incus reconciliation units. The Home Manager `programs.aginctus` module installs a CLI and nonsecret configuration; it does not yet provide a privileged host executor. This review must preserve that split and all existing #437/#439/#440/#441 write gates.
- Incus upstream distinguishes the full-access `incus-admin` local socket from restricted `incus` per-user-project access; restricted project-scoped TLS client certificates are also supported. A client selecting `--project` or a client SDK calling `UseProject` is **not** equivalent to a restricted identity.
- Incus `features.*` controls whether some project objects are isolated or inherited from `default`; `features.networks=true` requires OVN according to upstream docs. Aginctus's current host-shared bridge design means Incus project containment cannot independently enforce NixOS firewall or underlying bridge lifecycle.
- Incus TLS client authorization is configurable. The upstream docs warn that misrouting `authorization.client.tls-restricted` away from the `tls` evaluator can bypass per-certificate project lists. Test effective server authorization routing, not just certificate metadata.

These are code/documentation observations, **not** evidence that an operator principal can safely perform scoped writes on the pinned Incus server. Verify the installed daemon version and server configuration in a *dedicated disposable NixOS VM*, never on the pilot host.

Official upstream references (configuration and behavior may vary with the pinned server version):

- https://linuxcontainers.org/incus/docs/main/authorization/
- https://linuxcontainers.org/incus/docs/main/explanation/projects/
- https://linuxcontainers.org/incus/docs/main/reference/projects/
- https://linuxcontainers.org/incus/docs/main/howto/projects_confine/

## Principals and assets

| Principal | Trust boundary | Acceptable authority |
| --- | --- | --- |
| Host administrator/NixOS system actor | OS root; may administer daemon, bridges, storage pools and firewall | Host policy and deliberately system-owned Aginctus operations; cannot silently adopt another operator's credential owner |
| Home Manager operator | Untrusted user input and user services relative to host root | Explicitly delegated tenant Incus project only; no host-wide Incus admin socket |
| State service | Persistent revisions and operation metadata | Read/compare and narrowly authenticated CAS within authorized scope; no generic Incus SDK/root socket |
| Agent workload | Potentially compromised instance/process | No host administrative credentials, arbitrary Incus API writes or secrets of other domains |
| External/remote client | Client certificate/transport identity | Only verified project-scoped API actions, no ambient project privilege |

Host capabilities (Incus daemon config, storage pool provisioning, Linux bridges, NixOS nftables/firewalld policy) are **administrator domain**. Tenant resources (project-scoped instances, permitted images/profiles/volumes, runtime workload selection) may eventually be delegated when the tested Incus restrictions allow it. Credential ownership (LiteLLM OAuth refresh, Herdr SSH identities) remains with existing runtime owners, not the state service or project name.

## Alternatives to decide in #704

| Model | Advantage | Core risk |
| --- | --- | --- |
| **Native restricted Incus project identity** (recommended first test) | Incus enforces isolation, independent of Aginctus JSON merging | Shared host networks, inherited features, local-socket bypass, TLS deployment/rotation |
| **Separate host/project accepted authorities** | No privileged root/user config-tree merge | Capability references, consistent planning, recovery after one authority is unavailable |
| **Single accepted revision with server-scoped CAS** | Coherent revision and history | Confused-deputy privilege escalation from a bug in merge/normalization/ownership checks |
| **Hybrid composite view** with separate scoped revisions | Strong boundaries with unified CLI read view | Revision pinning, cross-domain dependencies and failure semantics |

A logical *combined status* need not imply one privileged config tree or writable database. The accepted-state service may expose only a composite read view of separate independently authorized revision domains. Avoid locking the persistence/schema work to a single ownership topology before this gate.

## Disposable Incus experiments: mandatory before cross-authority writes

**Environment:** dedicated NixOS VM with its own Incus daemon and separate virtual disk, consistent with the testenv isolation strategy. **Do not run** these experiments against the pilot host/daemon, and never grant an agent process live `incus-admin` membership. Keep the VM's networking isolated; if restricted TLS is tested, bind the Incus endpoint only to the disposable test network/loopback and use test-only credentials removed with the VM.

1. **Provision by root:** explicitly create a test project with `restricted=true`, isolated images/profiles/volumes, the intended `features.networks=false` bridge arrangement, and the least necessary `restricted.*` allowances. Record the *observed* server config rather than assuming feature defaults. Test private volume use and NIC attachment to the approved shared bridge.
2. **Confined operator identity:** establish one genuinely restricted client identity (project-scoped TLS certificate or supported per-user `incus` socket) **without access to the full-admin local socket**. For the per-user socket path, prove the dynamically selected project maps to Aginctus's explicit project identity; do not assume the two match. Verify `authorization.client.*` routing and any role/trust configuration.
3. **Allowed positive actions:** within the delegated project, test only expected instance create/read/update, allowed image/alias operations, bounded volume attachment, and read-only state inspection. Confirm capabilities needed by current native workloads before choosing a transport.
4. **Denied negative actions:** attempt to observe/mutate a foreign project, create/modify server config, change project restrictions, create host network/bridge, mutate storage pools, attach arbitrary host filesystem or devices, change another project's image/volume, and use unapproved privileged container settings. Named foreign-resource reads and forbidden mutations must fail. Collection reads may succeed only with verified server-side filtering: populate recognizable foreign resources, prove administrator visibility and allowed-scope positive controls, and assert their absence through the restricted client's CLI and raw API (including recursion and all-projects selectors). See the [executed evidence](incus-project-authority-evidence.md). Any disclosure or unexpected mutation success is an architectural blocker, not an invitation to add CLI filtering.
5. **Escalation/confused deputy:** attempt nested/dotted privileged fields, case aliases, duplicate JSON keys, null/omission/defaulting, config-source precedence crossing, project selector switching, forged owner labels/role flags, stale CAS and schema-version changes against the state API/adapter. Verify no system actor consumes an untrusted user raw merged config object.
6. **Lifecycle/failure:** restart Incus and state service; revoke operator credential while reconciling; change a project feature after an accepted revision; simulate missing bridge/volume and report unobservable or unauthorized status. Preserve protected data; no implicit adoption or destructive fallback.
7. **Evidence:** store concise environment versions, positive/negative result matrix, expected/observed error classes, authorized assertions and redacted logs. No secret values or private keys in test evidence.

**Acceptance threshold:** if restricted project credentials cannot safely operate required workload flows with shared bridges and volumes, choose the separate-authority approach or keep operator execution root-owned with explicit human/host authorization. Do **not** grant broad local Unix-socket access to make tests pass.

## Implementation constraints while gate remains open

- Allowed: typed nonsecret selection/candidate contracts (#694), read-only status/diff (#697), independent storage/bootstrap feasibility (#695), isolated single-principal tests.
- Blocked: cross-principal root/user JSON merging, user-triggered system reconcile proxies, administrative config promotion from user input, implicit authority transfer, or state-service possession of full Incus admin credentials.
- Future API: principal identity + explicit project and ownership domain must be checked by an authority **outside** untrusted config. A role flag or existing resource marker is not proof.
- Future status: distinguish **unauthorized**, **unobserved**, **missing** and **drifted**. No hidden field may silently become `unchanged`.
- Review order: prove transport and project restriction behavior, then choose authority layout, then implement state-service authorization (#696), then enable any cross-domain reconciliation (#698/#699).

## Disposition to record after experiments

The implementing PR or issue update must include a decision table: tested server version and configuration, effective operator client identity, allowed/denied operations, suitability for host-shared bridge/volume flows, consequences for accepted revision layout, backup/restore and migration, and explicit residual risk acceptance. Until then, **#704 remains open and cross-authority writes blocked**.
