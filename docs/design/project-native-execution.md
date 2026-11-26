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

The state service takes the accepted-pointer lock for the environment and a
conservative project-wide writer lock: accepted instances in different
environments can collide by name. The coordinator checks Incus trust, current
policy and the selected revision. It must durably record uncertainty before
granting each submission. The executor uses its own Incus credentials and must
verify ownership and terminal effects. The server never issues an Incus
mutation. Submission acknowledgement is evidence from the cooperating
executor, not independent proof of readiness or convergence.

There is no lease-based takeover. A disconnect can leave a session held; a
restart releases the in-memory pin but refuses new native writers across the
project if any earlier operation is still open or uncertain. A delayed request
cannot reuse an old session after restart. A successful no-op produces no
effect record. The workspace fixture recovery mechanism does not constitute a
production recovery authorization. Production inspection and read-only
verification of terminal Incus operations, including the submission window
before a daemon operation ID has been recorded, are still required. There is
no time-based or operator-asserted override in this draft.
