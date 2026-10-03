# ADR 0002: Use a shared local socket for the first host-to-workload control path

- Status: Accepted
- Date: 2026-10-03

## Context

The first Aginctus milestone is a Herdr client or host-side control process interacting with a Herdr server running inside an Incus-managed container.

Herdr exposes a local socket API. On Unix, that API is served over a Unix-domain socket, and the socket path can be overridden with `HERDR_SOCKET_PATH`.

Because an Incus system container shares the host kernel, a Unix-domain socket created inside a host-mounted directory can be visible to both the host and the container. That allows Aginctus to reach Herdr without introducing network exposure solely for orchestration.

## Decision

The first host-to-workload control endpoint will be a Unix-domain socket stored in an Aginctus-managed runtime directory shared between the Incus host and the container.

Aginctus will:

- create a workload-scoped host runtime directory;
- mount that directory into the container;
- configure Herdr to place its socket there;
- expose the realized socket path to host-side tooling;
- apply restrictive ownership and permissions;
- clean up the runtime directory with the workload.

The socket path is an implementation of a higher-level control-endpoint abstraction, not a requirement that all workloads or isolation modes use Unix sockets.

SSH remains a future transport option for VMs, remote Incus hosts, and environments where shared local IPC is unavailable.

## Consequences

- The first usable Herdr environment does not require an SSH server, SSH keys, host-key management, or a host-to-guest network path.
- No Herdr application port needs to be exposed.
- Access to the socket confers control over the Herdr session, so its directory and permissions are security-sensitive.
- The initial implementation can focus on container orchestration and shared runtime state before introducing VM-specific transport.
- VM support requires another transport because the guest does not share the host kernel.
- The workload model should express control access independently from the concrete transport so SSH, vsock, HTTP, or other mechanisms can be added later.
