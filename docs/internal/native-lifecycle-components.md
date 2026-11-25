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

For the [D fixture join](native-authority-fixture.md), construct a
`nativelifecycle.LocalComposer` with the enrolled descriptor, independently
approved bridge observer/policy and attachment verifier, and a restricted
standalone Incus SDK connection plus trusted TLS pin. Set
`LocalNativeExecution.BindLifecycle` to `composer.BindLifecycle`. This method
derives workload runtime/pool/bridge and the Herdr spec from the accepted body
along with the pinned identities, fingerprints, profiles, and running intent.
The graph selects logical stages only. Reconcile requires explicit empty
profiles. `Build` returns the actual graph for D's exact comparison.
`VerifyPlan`, `BeforeEffect`, the read-check policy, live exclusion and durable
outcome handling belong to the authority/composition layer. Bindings with
observation state are invocation-local; build them anew for another run.

The fixture also supports `LocalNativeExecution.ExecuteLocalTeardown` through
`LocalComposer.BindTeardown`. Select either graceful stop or removal for the
whole invocation. Supply exact logical stage IDs and independently verified
live instance incarnations, plus the retained volume incarnation for Herdr.
The accepted candidate supplies the actual instance and volume intent. A
dedicated `AuthorizeTeardown` policy checks the operation, full selection and
each exact effect, including the recheck after durable journaling. Removal
requires a separate invocation against already stopped instances. No bridge,
gateway, extra instance or volume mutation can enter the teardown graph.
This path is internal to the same-UID fixture; production authority is separate.

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
The disposable C/D fixture exercises create, start, dry-run, idempotence,
graceful stop, and removal on real Incus. It writes a sentinel through the
Herdr guest mount and reads the same bytes from the detached volume after
removal, with the same custom-volume incarnation. The bridge
evidence is a read-only snapshot exported by the guest fixture administrator;
the execution principal only gets restricted TLS. The guest image is a minimal
container image. This does not exercise SSH/Herdr application readiness, VM
workload isolation, or production fencing. The production CLI is not activated.

```sh
nix develop --offline -c go test -race ./internal/nativelifecycle ./internal/orchestration/...
env -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK \
  timeout --kill-after=15s 12m nix --quiet build --offline --no-update-lock-file --no-link -L \
  .#vm-harness-native-joined-lifecycle
```
