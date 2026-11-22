# Project-state service: administrative attachment preflight

Status: read-only implementation gate, **not** service authentication or permission to activate production state promotion.

The project-owned service topology requires one already-provisioned custom
filesystem volume attached exclusively to one dedicated instance in the
verified Incus project. `environment.ObserveLocalProjectServiceAttachment`
performs read-only local-host administrative inspection; the injected
`VerifyProjectServiceAttachment` provides the same checks for isolated tests.

The preflight requires an explicit nondefault project, instance, pool,
custom volume, disk device and absolute mount path. It checks:

- the local Incus connection selected the exact project;
- the project carries the Aginctus-owned deployment-project policy and
  isolates project storage volumes;
- the instance and volume have independent Aginctus state-service ownership
  markers, and a single expected disk device attaches the exact volume;
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

**Trust boundary:** This helper uses the local host Incus Unix socket, which
may have administrator permissions. It is only for an already authorized
host-side inspection path; NEVER expose it through an untrusted user RPC,
Home Manager activation, state-service request handler, or arbitrary candidate
file. A passing preflight does not establish that a service process lacks
administrator credentials, owns the mount exclusively at the kernel level,
can survive backup/restore, or authenticates restricted project principals.
Continue #695/#696/#701/#704 real-Incus negative and recovery evidence.
