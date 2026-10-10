set shell := ["bash", "-euo", "pipefail", "-c"]

mod build '.just/build.just'
mod update '.just/update.just'

# `just fmt` runs both formatters; pass `go` or `nix` to select one.
fmt target="all":
    @case "{{target}}" in \
      go) gofmt -w cmd internal ;; \
      nix) nix fmt ;; \
      all) gofmt -w cmd internal; nix fmt ;; \
      *) echo "unknown formatter: {{target}} (expected go or nix)" >&2; exit 2 ;; \
    esac

test:
    go test ./...

vet:
    go vet ./...

check: modules test vet

modules:
    go mod verify

# Integration tests run only on disposable Incus. By default each test
# deletes its uniquely named project (including test containers and volumes).
# --retain preserves projects for inspection; --unsafe provides opt-in flags.
integration-test *args:
    @set -euo pipefail; flags='{{args}}'; export AGINCTUS_INCUS_RETAIN=0; for flag in $flags; do case "$flag" in --unsafe) export AGINCTUS_INCUS_INTEGRATION=1 AGINCTUS_INCUS_DISPOSABLE=YES ;; --retain) export AGINCTUS_INCUS_RETAIN=1 ;; *) echo "Unknown integration-test option: $flag (expected --unsafe or --retain)" >&2; exit 2 ;; esac; done; if [[ "${AGINCTUS_INCUS_INTEGRATION:-}" != "1" || "${AGINCTUS_INCUS_DISPOSABLE:-}" != "YES" ]]; then echo "Use 'just integration-test --unsafe' on a disposable host, or set AGINCTUS_INCUS_INTEGRATION=1 and AGINCTUS_INCUS_DISPOSABLE=YES" >&2; exit 1; fi; go test ./internal/orchestration/drivers/compose -run '^TestIncusCompose.*Integration$' -count=1 -v -timeout 12m
