# Native lifecycle component handoff (#153)

`internal/nativelifecycle.Build` returns the C portion of a `NativePlan` and
its exact `NativeResourceBinding` map. Compose these with the other stacks
before passing them to `Engine.ExecuteNative`. The builder neither opens a
connection nor grants authority. It requires a pinned accepted candidate,
the enrolled server/project descriptor, an independently supplied TLS pin,
and an exclusive restricted SDK session. The project must have the existing
native-compatible policy: private images/profiles/custom volumes, the shared
host bridge, and Aginctus project markers. Markers are collision evidence.

| Logical stage | Required prerequisites in the C fragment | Possible writes |
| --- | --- | --- |
| `management-network` | Independent verification of the host capability on the enrolled server | None |
| selected workload ID | `management-network` for reconcile/start | Create stopped, update managed config/description, start, graceful stop, or explicit remove |
| `herdr-system-state` | `management-network` for reconcile/start | Explicitly enabled initial custom-volume creation only |
| `herdr-client` | `herdr-system-state` | Create stopped, update managed config/description, start, graceful stop, or explicit remove |

Every prerequisite has a required read-only postcondition. An independent
attachment verifier checks the exact instance, pool and bridge during planning
and again at use. Stop/remove do not need a functioning bridge to operate.
Host administrators can use `managementnetwork.Spec.ExecuteNativeHost` with
their own local host-scoped client, guard and exclusion; this path refuses
adoption and does not touch firewall, pools, or project policy.

The composer must independently authenticate the revision, selection, bound
typed specs, operations, all graph edges and each exact effect target. Today’s
accepted workload body pins the logical ID, instance name/type, fingerprint,
profiles and running intent; it does not pin the extra attachment/runtime
policy or the Herdr spec. Do not source these missing inputs from ambient
configuration. `VerifyPlan`, `BeforeEffect`, the read-check policy, live
exclusion and durable outcome handling belong to the authority/composition
layer. Hold the pin and fence for the entire invocation. Bindings with
observation state are invocation-local; build them anew for another run.

Creation uses a project-local immutable image fingerprint and an explicit empty
profile list. Profile/attachment/image replacement drift blocks reconciliation;
updates retain omitted config and devices. Workload removal requires an explicit
operation and a stopped, non-ephemeral, unprotected incarnation without backups
or snapshots. It deletes the instance and its root disk (including guest-local
data); it has no custom-volume or host workspace deletion surface. Herdr uses a
scoped custom state disk for its SSH identity and configuration. An existing
Herdr instance never gets a fresh empty disk; a retained disk must still match
the verified incarnation at use and after the instance operation. A first
bootstrap needs the explicit `CreateState` selection and a separately approved
volume create effect. Labels cannot prove that a missing volume has no history.

Incus start/stop and delete are not conditional on an instance ETag server-side
in the pinned SDK/server implementation. Fresh observations and the cooperating
writer fence are mandatory; they do not exclude unrelated administrators.
Snapshots, backups, custom-volume usage, application readiness and SSH/Herdr
guest identity behavior require independent disposable-Incus evidence before
cutover. A verified runtime state is not application readiness. Until the
authority and cross-stack gates are met this fragment has no production CLI
activation, bootstrap or migration behavior.
