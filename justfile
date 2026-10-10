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

# Requires explicit opt-in; --unsafe is shorthand for the two disposable-host
# acknowledgment variables. The test retains projects for manual cleanup.
integration-test *args:
    @set -euo pipefail; flag='{{args}}'; if [[ "$flag" == "--unsafe" ]]; then export AGINCTUS_INCUS_INTEGRATION=1 AGINCTUS_INCUS_DISPOSABLE=YES; elif [[ -n "$flag" ]]; then echo "Unknown integration-test option: $flag (expected --unsafe)" >&2; exit 2; fi; if [[ "${AGINCTUS_INCUS_INTEGRATION:-}" != "1" || "${AGINCTUS_INCUS_DISPOSABLE:-}" != "YES" ]]; then echo "Use 'just integration-test --unsafe' on a disposable host, or set AGINCTUS_INCUS_INTEGRATION=1 and AGINCTUS_INCUS_DISPOSABLE=YES" >&2; exit 1; fi; go test ./internal/orchestration/drivers/compose -run '^TestIncusCompose.*Integration$' -count=1 -v -timeout 12m
