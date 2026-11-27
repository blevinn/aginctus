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
For the approved same-daemon protected topology, select the physical project
with `--project` and the distinct workload project with `--logical-project`.
Both must be independently administered selections on the pinned daemon.
The physical project, service guest and volume must carry the exact
`user.aginctus.logical-project` marker. Both projects must retain their
restricted and isolated project policies. Omitting the logical project is
only for the administrator-controlled same-project exercise; it refuses
resources carrying protected-placement markers.

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

The installed environment-state VM scenario now provisions its service guest
and accepted volume in the protected project, while retaining workload-project
TLS enrollment, the logical store binding and workload execution. Direct
logical-peer reads of the existing protected guest and volume must be denied.
This is not a metadata-only service certificate policy or complete delegated
isolation proof; the remaining #704 data-plane, trust and revocation gates
remain binding.
