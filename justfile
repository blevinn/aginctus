set shell := ["bash", "-euo", "pipefail", "-c"]

mod build '.just/build.just'
mod update '.just/update.just'

fmt:
    gofmt -w cmd internal
    nix fmt

test:
    go test ./...

vet:
    go vet ./...

check: modules test vet

modules:
    go mod verify

# Intentionally destructive to disposable Incus only; test suite is skipped
# unless both acknowledgements are provided explicitly.
integration-test:
    test "${AGINCTUS_INCUS_INTEGRATION:-}" = "1" || { echo "Set AGINCTUS_INCUS_INTEGRATION=1 to enable Incus integration tests" >&2; exit 1; }
    test "${AGINCTUS_INCUS_DISPOSABLE:-}" = "YES" || { echo "Set AGINCTUS_INCUS_DISPOSABLE=YES only on a disposable Incus host" >&2; exit 1; }
    go test ./internal/orchestration/drivers/compose -run '^TestIncusComposeReconciliationIntegration$' -count=1 -v -timeout 12m
