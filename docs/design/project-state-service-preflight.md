# Project-state service: administrative attachment preflight

Status: read-only implementation gate, **not** service authentication or permission to activate production state promotion.

The project-owned service topology requires one already-provisioned custom
filesystem volume attached exclusively to one dedicated instance in the
verified Incus project. `environment.ObserveLocalProjectServiceAttachment`
performs read-only local-host administrative inspection; the injected
`VerifyProjectServiceAttachment` provides the same checks for isolated tests.

Run `aginctus internal project-state-attachment --help` for the installed
entry point. Invoke it directly as an administrator on the daemon host. Supply
the independently provisioned `sha256:` server certificate fingerprint, exact
nondefault project, instance, pool, custom volume, disk device and absolute
mount path. Supply the exact managed network if the guest has an `eth0` NIC.
The command checks:

- the fixed local Incus socket returned the independently pinned server
  fingerprint and exact project;
- the project carries the Aginctus-owned deployment-project policy and
  isolates project storage volumes;
- the container and volume have independent Aginctus state-service ownership
  markers, and the expected disk is present in both local and effective devices;
- expanded profile config does not introduce raw or privileged settings, and
  the only effective devices are a pool-backed root disk, the exact state disk,
  and the explicitly selected managed NIC if requested;
- the Incus `UsedBy` inventory contains exactly one project-qualified
  consumer, that instance, and no unexpected custom volume is attached;
- ETags, project policy, connection identity and attachment metadata remain
  consistent through repeat observations.

The proposed new resource markers are
`user.aginctus.resource=project-state-service` for the instance and
`user.aginctus.resource=project-state-volume` for the volume, both with
`user.aginctus.managed=true`. These are **not** enrollment or authorization
grants. Confirm marker compatibility with the disposable Incus fixture before
using this preflight as a release condition.

**Trust boundary:** This helper uses `/var/lib/incus/unix.socket` directly,
without a remote or ambient socket override. It is only for an already authorized
host-side inspection path; NEVER expose it through an untrusted user RPC,
Home Manager activation, state-service request handler, or arbitrary candidate
file. A passing preflight does not establish that a service process lacks
administrator credentials, owns the mount exclusively at the kernel level,
can survive backup/restore, or authenticates restricted project principals.
Repeated observations detect visible drift; they do not hold a writer fence.
See the [administrator-only deployment and isolation gate](project-state-service-topology.md#administrator-controlled-deployment-only)
before configuring any listener.
Continue #695/#696/#701/#704 real-Incus negative and recovery evidence.
