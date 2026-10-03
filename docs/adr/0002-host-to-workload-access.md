# ADR 0002: Use SSH for the first host-to-workload access path

- Status: Accepted
- Date: 2026-10-03

## Context

The first Aginctus milestone is a Herdr client running on the Incus host and a Herdr server running inside an Incus-managed workload.

Herdr's supported remote attach workflow uses SSH. The remote host owns the Herdr server and running panes while the local Herdr client renders the interface.

Aginctus needs a networking model that enables this path without prematurely defining general public ingress.

## Decision

The first host-to-workload endpoint will be SSH from the Incus host to the managed instance.

Aginctus will treat this as an explicit access path and will be responsible for enough endpoint discovery and client configuration that users do not need to manually inspect Incus-assigned addresses.

The initial implementation should prefer direct host-to-instance reachability on an Incus-managed network. Public forwarding is not required.

SSH host verification remains enabled. Aginctus must not solve bootstrap convenience by globally disabling host-key checks.

## Consequences

- No Herdr-specific application port needs to be published.
- The first networking implementation focuses on host-to-instance reachability, SSH identity, host verification, and endpoint discovery.
- The workload model should express access intent without encoding an Incus IP address.
- Later endpoint implementations may use Incus proxy devices, HTTP ingress, or other transports without redefining the workload itself.
- General guest egress and workload-to-workload networking remain separate policy concerns.
