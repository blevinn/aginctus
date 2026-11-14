# Lifecycle controller authority: boundary and implementation contract

Status: **provisional engineering contract; no delegation sign-off**. Tracks [#441](https://github.com/blevinn/aginctus/issues/441), [#704](https://github.com/blevinn/aginctus/issues/704), and [#426](https://github.com/blevinn/aginctus/issues/426). Read alongside [the project authority threat model](incus-project-authority-gate.md) and [ADR 0004](../adr/0004-three-layer-state.md).

## What is already established

Standalone Home Manager owns the operator CLI and nonsecret *configured candidate*, not live Incus resources. NixOS owns the daemon, root firewall, host bridge, and preexisting protected gateway state. A Home Manager switch, user login, package change, or environment change must not promote accepted state or reconcile resources.

The read-only `operator preflight`, `units-audit`, `package-audit`, and `state-audit` commands answer different questions. A successful result from **any combination** of them is **not** authorization for a Home Manager writer:

- A restricted TLS certificate may permit project reads without project writes; an unrestricted credential may be further constrained by the server authorizer.
- A project name, `--project` selection, Incus ownership label, EUID, `incus` group, or daemon-local Unix socket is not proof of a confined principal.
- A known-unit allowlist does not observe other users, manual Incus clients, renamed units, or noncooperating writers. A project-level cooperative lease only serializes participating processes in its namespace.
- Declared Nix package equality and the currently running CLI path do not establish the revision of an independently running service.
- XDG user state and `/var/lib/aginctus/state` are different credential owners. Unreadable protected state is **unverified**, never **absent**.
- Project `features.networks=false` inherits default networking. A project with `restricted=true` and unrestricted network access is not necessarily confined to its own Linux bridge lifecycle (observed in #633). `features.networks=true` changes the resource model and must be tested with explicit host-shared network allowances before any migration.

## Resource/controller ownership matrix

| Resource or action | Initial authority | May Home Manager own it now? |
| --- | --- | --- |
| Daemon, storage pools, shared bridge, NixOS firewall, firewall journal | System administrator / NixOS | **No** |
| Root gateway secrets, credential rotation and system gateway state | Protected system owner | **No**; separate explicit state migration required |
| Selected project-scoped instances, images, profiles and volumes | Explicit existing controller/owner | **No automatic handoff**; only individually confirmed existing scoped CLI commands are currently supported |
| Home Manager package and nonsecret candidate JSON | Home Manager user | **Yes**, no Incus effects |
| Accepted revision and promotion CAS | Future separately authenticated state authority | **Not yet available**; #693/#696/#699 |
| Cross-principal ownership transfer | Explicit approved handoff | **Blocked** pending #704 and #442 |

## Required write-time gate for a future lifecycle service

The executor, **not the Nix module, user configuration, shell wrapper or state-service caller**, must verify all of these for **each selected resource operation**:

1. **Authenticated principal and delegation:** use server-verifiable principal credentials and a host-established capability boundary. Never infer it from user-controlled role or owner fields. A project-scoped local administrator socket is still an administrator socket.
2. **Exact domain:** bind the effective principal to a preapproved deployment/project/resource/action. Reject ambient project fallback, cross-project references, default/shared host-resource operations and unknown scopes.
3. **Accepted intent:** use the immutable accepted revision (once #693 is implemented) rather than changes in HM/JSON/environment. Check accepted revision, resource selector and active ownership together; a candidate is not an accepted revision.
4. **Exclusive controller ownership:** verify a trusted owner claim and a fencing/lease mechanism honored by *all authorized* system and user writers. Known-systemd-unit absence and the current cooperative lease alone cannot establish this.
5. **Independent resource policy:** observe current Incus resource identity, provenance, immutable fields, attached volumes and effective project restrictions. Reject foreign, duplicated, missing, inconsistent or unobservable claims.
6. **Mutation-boundary recheck:** hold the relevant exclusion mechanism across observe, plan, validate, conditional Incus write, and verification. Recheck authoritative principal/scope/controller/accepted revision and the Incus ETag or equivalent immediately before the write. Treat uncertain completion as uncertain, not as rolled back.
7. **Secrets and host policy:** an unprivileged execution path cannot read protected root secrets, alter host bridge/firewall policy, or induce a privileged host process to execute unfiltered user-supplied configuration.

A successful read-only preflight is therefore **necessary diagnostic evidence, never sufficient write evidence**. The executor must fail closed when a claim is unknown; there is no `sudo` fallback. An invocation that is merely *serialized* is not necessarily *authorized*.

## Implementation boundary after the local-pinning slice

The merged #772/#776 same-UID accepted revision lock, followed by #814
(selected accepted-workload snapshots) and #816/#818 (durable immutable
**unverified** local operation-intention records and process-exit recovery),
are deliberately limited building blocks. The new PRs are pending review
and are not an authenticated operation service or an Incus write path.

- **What these cover:** one pinned accepted workload, local cooperating
  promotion exclusion, private durable operation identities, and conservative
  reporting of unresolved outcomes on restart.
- **What they cannot cover:** authentication across root/user trust domains,
  an authoritative controller grant, noncooperating or other-user writer
  exclusion, in-flight Incus credential revocation, a root-owned accepted
  state store, successful mutation verification or a signed audit history.
- **Required before #438/#439:** implement an authenticated project-scoped
  executor using the restricted TLS policy proved in disposable Incus;
  enforce a trusted owner/lease and resource-level policy at the *actual*
  write boundary; bind execution to the accepted snapshot and a durable
  journal; test partial/crash behavior, revocation, external-writer conflict
  and reversible ownership handoff in the VM.

No NixOS/HM module activates these APIs. A successful `SnapshotSelectedWorkload`
or `RecordLocalUnverifiedAttempt` call does not imply the caller can mutate Incus.

## Branching choices still open

[#704](https://github.com/blevinn/aginctus/issues/704) must choose and validate whether the operational boundary is (a) project-native credentials with network/volume restrictions, (b) separately authoritative host/project accepted revisions, (c) an independently authenticated single state service with scoped CAS, or (d) a hybrid composite. A composite read view can be shared without merging untrusted user input into privileged host policy.

Do **not** encode a permanent root/user merge rule, deploy an HM writer, implement root-proxy calls, or force `features.networks=true` on existing installations before that choice. The current project-policy flags in #200 are not secretly a capability grant.

## Validation and closure

- **Read-only policy evidence** should distinguish a demonstrably unsafe policy, a denied/unobservable policy and a *candidate* confined policy. Even the last state is not execution authorization.
- **Disposable Incus** must verify a restricted TLS client with exact project selection, allowed project observations, denied foreign/default writes, host shared-bridge mutation denial, pool/project-policy denial, and runtime daemon authorization routing. Observe the actual daemon, not certificate metadata alone.
- **Real controller conflict tests** must cover both managers, an inactive installed competing unit, another-user/manual writer, and losing/revoked ownership during an Incus write. Include partial or canceled execution; no fabricated rollback.
- **End-to-end handoff** must verify backup, quiescence, a single active owner, preserved database/workload volumes and identities, and explicit rollback in a disposable VM before #442 closes.

The above is a **design and acceptance contract**, not code-level controller fencing, project delegation approval, or a grant for #438/#439. Tracking issues remain open until the corresponding real executor and negative tests pass.
