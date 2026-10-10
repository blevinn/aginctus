#!/usr/bin/env bash
set -euo pipefail

# A read-only module integrity gate: do not let Go silently fix incomplete sums.
export GOFLAGS="${GOFLAGS:-} -mod=readonly"
go mod verify
go test ./...
go build ./cmd/aginctus
