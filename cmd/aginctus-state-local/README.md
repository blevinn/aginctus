# aginctus-state-local

Offline, single-principal proof-of-concept tool for local accepted-state revision inspection and acceptance. It never connects to Incus, reconciles workloads, or acts as a privileged proxy.

## Usage

```sh
go run ./cmd/aginctus-state-local init --state-dir /absolute/private/state
go run ./cmd/aginctus-state-local status --state-dir /absolute/private/state --deployment DEPLOYMENT --project PROJECT
go run ./cmd/aginctus-state-local diff --state-dir /absolute/private/state --deployment DEPLOYMENT --project PROJECT --candidate candidate.json
go run ./cmd/aginctus-state-local accept --state-dir /absolute/private/state --deployment DEPLOYMENT --project PROJECT --candidate candidate.json --expected-revision none --confirm
```

`init` creates the private store; other actions require an explicit deployment and project. `diff` and `accept` require a pinned candidate JSON file. For `accept`, supply `--expected-revision none` only for the first acceptance; otherwise use the current revision ID. Acceptance performs a compare-and-swap against the local store, **not** infrastructure reconciliation. Output is JSON. See [local accepted-state operations](../../docs/operations/accepted-state-local.md).
