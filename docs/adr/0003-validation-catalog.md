# ADR 0003: Go-owned validation catalog and staged tooling convergence

Status: accepted for v0.2 (#630; Phase A)

## Context

Aginctus has stable, safety-conscious hermetic reporting and several VM
execution surfaces, but suite/scenario names and policy recur in Python, Go,
Nix and Just. Repeated inventories drift and are not proof of equivalent
privileges. The native-oci feasibility spike, for example, is selectable by
the standalone Python runner but intentionally absent from public Go testenv
discovery and the existing v1 VM evidence reader.

## Decision

The dependency-light Go package internal/validation/catalog owns typed suite
and VM scenario metadata. It performs no I/O or execution. The existing
operator-only testenv CLI projects its public list from that catalog without
changing JSON shape, descriptions, names or execution behavior.

The catalog records tier, public/operator-vs-internal surface, fixed Nix
target, networked Nix test-driver selection and whether the v1 evidence
adapter recognizes the scenario. Entries DO NOT authorize execution.
Specifically native-oci remains internal/experimental, networked and
not v1-evidence-compatible; do not quietly expose or certify it.

Python, Just and Nix entrypoints remain static until deliberately migrated.
Hostless tests enforce their parity, including explicit internal exclusions.
Dynamic scenario discovery, generated arbitrary shell commands and unbounded
user-selected Nix attributes are not appropriate for this boundary.

The eight hermetic suite IDs and order, existing commands and JSON v1 output
stay stable. The catalog additionally declares fixed developer-CI authority,
hermetic JSON v1 evidence contract and a 3600-second maximum per-suite deadline.
The Go runner enforces this deadline (including cancellation as an execution
error, not a test assertion failure). VM metadata declares explicit operator
selection and the original disposable-VM evidence contract; it does not
acquire a VM executor, authorize persistent workers, or grant new privileges.
These are compatibility policy descriptors, not permission tokens.

## Execution boundaries

- Hermetic: explicit developer/CI invocation with live and Incus opt-ins
  scrubbed from each child environment.
- Batch NixOS VM: explicit operator on disposable worker, not agent-safe
  persistent VM admission or arbitrary host shell.
- Persistent testenv: existing internal/testenv backend plus dedicated-worker
  identity, admission, ownership and independent recovery.
- Pilot/live provider: separately authorized, never implicit in catalog
  discovery, hermetic checks or synthetic evidence.

A passed synthetic fixture does not prove authenticated upstream inference.
Unavailable independent observations remain not_run/not_verified. No local
signing system is introduced.

## Migration

A. Catalog, parity checks and documentation.
B. Add Go hermetic runner; shadow-test against existing Python reporting.
C. Adopt Go reporting through Just/CI only after parity; selectively converge
   read-only evidence adapters without weakening private-file checks.
D. Move product-owned administration to Go as product APIs mature. Keep
   Python-native LiteLLM hooks/probes and NixOS test-driver integration.

Do not create a second VM backend, grant agents host powers, rewrite CLI
parsing, change production gateway behavior or run pilot/privileged tests.

Related: #491, #425, #457, #475, #293, #296, #514, #630.
