# Project-state native writer coordination (draft)

The existing `aginctus serve project-state` listener defaults to no execution
route. The optional `executionPolicyFile` is for isolated development and
review only, pending [deployment isolation](project-state-service-topology.md)
and an installed execution and recovery consumer. Do not enable the route in
a delegated or production deployment.

The guest administrator owns the policy file and every directory in its path;
none may be writable by the service or another nonroot identity. Its version
is `aginctus.dev/project-execution-policy/v1alpha1`. Each `grants` entry has
`peerSha256` (DER certificate hash), `serverId`, `project`, `environmentId`,
`revisionId`, `workloadId`, and `operation`. Reconcile authorizes create and
start on the exact selected workload. Stop and removal each require a distinct
grant. Promotion policy does not grant execution.

The separate `inspect` operation grant is read-only and matches the exact
revision and workload. With the same pinned operator configuration and explicit
project/environment selectors, run `state operations --workload ID --revision
REVISION -o json` for summaries or add an exact operation ID for effect records.
This also works for historical published revisions when explicitly granted.
The report omits writer certificate fingerprints; `unresolved` is a journal
classification, not live observation. An inspection grant cannot reconcile or
resolve an operation.

After an Incus SDK submission returns, the executor checks that the task
references the exact project and instance and persists its daemon UUID before
waiting. This is a correlation hint. A crash before that write, a lost
acknowledgement or an aged-out daemon record cannot establish that nothing was
submitted. No correlation write clears uncertainty.

The state service takes the accepted-pointer lock for the environment and a
conservative project-wide writer lock: accepted instances in different
environments can collide by name. The coordinator checks Incus trust, current
policy and the selected revision. It must durably record uncertainty before
granting each submission. The executor uses its own Incus credentials and must
verify ownership and terminal effects. The server never issues an Incus
mutation. Submission acknowledgement is evidence from the cooperating
executor, not independent proof of readiness or convergence.

The installed workload draft requires a separate root-owned host grant for the
exact instance, pool and bridge. Incus 7.5.1 exposes an inherited bridge to a
restricted project, but redacts its configuration (including ownership markers)
unless the caller can edit the network. The workload TLS principal is not given
that entitlement. For the disposable fixture, an administrator-owned systemd
Unix socket activates `aginctus internal workload-bridge-observe HOST_GRANT`.
The configured `bridgeSocket` permits the workload operator to request only a
fresh yes/no check. The helper accepts no request data, checks the live bridge
and independently pinned standalone Incus server over the fixed administrative
Unix socket, and returns only a digest of the exact grant on success. The
executor checks the socket peer UID and grant digest. It separately verifies the
restricted project's exact network and storage attachment policy over TLS.
This is a point-in-time observation; it does not lock the host bridge or expose
the administrative socket to the executor or the state service.

For a single workload, `state plan-native --workload ID --expect REVISION
--host-grant PATH -o json` runs the installed workload and bridge preflight in
read-only mode. It authenticates the same project and verifies that the
accepted revision still matches before returning. It does not open an
execution session, record effects, or reserve the revision; subsequent
reconciliation must obtain its own grant and recheck all preconditions.

There is no lease-based takeover. A disconnect can leave a session held; a
restart releases the in-memory pin but refuses new native writers across the
project if any earlier operation is still open or uncertain. A delayed request
cannot reuse an old session after restart. A successful no-op produces no
effect record. The workspace fixture recovery mechanism does not constitute a
production recovery authorization. Production inspection and read-only
verification of terminal Incus operations, including the submission window
before a daemon operation ID has been recorded, are still required. There is
no time-based or operator-asserted override in this draft.

### Initial terminal-task recovery policy

The hidden `state operations OPERATION_ID --workload ID --revision REV --recover -o json`
uses a separate exact `recover` execution-policy grant. The installed state
service requires no active coordination session, takes the same persistent
project and environment locks, and rechecks the current revision and grant.
It independently reads each acknowledged applied daemon task and requires a positive
successful task of the expected create/start kind for the exact instance,
followed by a current matching owned instance and stable incarnation. It
publishes the existing digest-bound recovery receipt; inspection reports that
receipt rather than an unresolved operation. It performs no Incus mutation.
This first policy cannot recover unacknowledged effects (even correlated ones), missing/expired task IDs, failed tasks,
stop/remove, or an unmatched resource. The original executor is assumed to
follow the coordination protocol, and unrelated administrators are not fenced.
Do not enable delegation on this basis: validate the pinned Incus task
vocabulary, operation retention, authorization and actual protected service
placement. A missing task or a denied API leaves recovery blocked.

### Explicit component coordination

The same state-service writer pin and durable journal now admit two narrowly
selected component grants: `gateway-deliver` with `workloadId=gateway-credentials`,
and `herdr-reconcile` with `workloadId=herdr-client`. These are operation-specific
selectors, not ordinary accepted workload IDs. They require the corresponding
accepted component and do not inherit ordinary workload grants. Component
inspection uses `gateway-inspect` or `herdr-inspect` respectively. The gateway
delivery action is a synchronous file handoff; its acknowledgement does not
require a daemon task ID, unlike instance operations. Lost acknowledgement
or partial delivery remains unresolved. A session check revalidates the
current grant and both locks without advancing the journal. These grants
coordinate only: they convey no physical Incus authority and do not enable
delegated execution. Gateway runtime and Herdr state creation are not granted.
### Installed Herdr resource lifecycle

The hidden `state reconcile --herdr --expect REV --host-grant FILE -o json`
uses the accepted Herdr domain, existing bridge observer, actual restricted
SDK and a separate `herdr-reconcile` grant. The host grant uses workloadId
herdr-client and exact Herdr instance/pool/network. An explicit
`--bootstrap-herdr-state` additionally requires accepted AllowStateBootstrap
and a distinct herdr-bootstrap grant. The synchronous volume create is
journaled as create-state and independently observed by the existing domain
binding; instance creates/starts record daemon UUIDs. Existing state is
retained and foreign or missing state is refused unless initial bootstrap was
authorized. Live `state plan-native --herdr` supports the same host grant,
expected revision and JSON output without a writer pin. This is resource
lifecycle only: SSH, workspace, application readiness, stop/remove and recovery
of component journals remain separate. No delegated activation approved.
