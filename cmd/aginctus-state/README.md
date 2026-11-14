# aginctus-state

Read-only, same-UID Unix-socket service exposing local accepted-state revisions. It does not accept revisions, reconcile Incus resources, or listen on the network.

## Usage

```sh
go run ./cmd/aginctus-state --state-dir /absolute/private/state --socket /absolute/private/run/state.sock
```

Both flags are required. `--state-dir` must identify a preinitialized private accepted-state directory; the socket must reside in an owned mode-0700 directory. The service runs until interrupted or terminated. This endpoint is not an authorization or cross-principal proxy. See [accepted-state operations](../../docs/operations/accepted-state-local.md).
