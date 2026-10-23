set shell := ["bash", "-euo", "pipefail", "-c"]

mod build '.just/build.just'
mod update '.just/update.just'
mod fmt '.just/fmt.just'

test:
    go test ./...

# Run the heavyweight NixOS firewall VM tests explicitly (x86_64-linux).
test-nix-firewall:
    nix build -L --no-link .#firewall-iptables-integration .#firewall-firewalld-integration .#firewall-nixos-nft-integration

# Boot a pinned real Incus guest using the shared VM fixture (#427).
# Always explicit: excluded from check-all and normal CI.
test-vm-smoke:
    python3 scripts/vm_harness/runner.py test smoke

# Inspect the same VM fixture with the interactive NixOS test driver.
# Run under a dedicated unprivileged worker identity, never on pilot host.
vm-debug:
    python3 scripts/vm_harness/runner.py debug smoke

# Hostless safety tests for runner command/environment selection.
test-vm-runner:
    python3 -m unittest discover -s scripts/vm_harness -p 'test_*.py' -v

# Builds and runs a disposable QEMU VM with real Incus and the packaged CLI.
# Unlike 'just test', this is a heavyweight integration check.
test-nix *args:
    bash tests/nixos-incus-vm.sh {{ args }}

vet:
    go vet ./...

check: modules test vet

# Safe aggregate, even when the caller has opted into live acceptance elsewhere.
check-all:
    env AGINCTUS_INCUS_INTEGRATION=0 AGINCTUS_INCUS_DISPOSABLE=NO AGINCTUS_ACCEPTANCE_LIVE= AGINCTUS_ACCEPTANCE_LIVE_HOOKS= ACCEPTANCE_TEST_FAIL= GOFLAGS= GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local just check check-lint test-python test-shell check-shell check-markdown-links check-nix

# Linters run with the pinned development shell tools. Each tool owns its
# selection: .golangci.yml for Go, ruff.toml for Python, and the file list
# itself for shellcheck.
check-lint: check-go-lint check-python-lint check-shell-lint

check-go-lint:
    golangci-lint run

check-python-lint:
    ruff check .

check-shell-lint:
    git ls-files -z -- '*.sh' | xargs -0 -r -n1 shellcheck

test-python:
    python3 -m unittest discover -s scripts/vm_harness -p 'test_*.py' -v
    python3 -m unittest discover -s guest/opencode -p 'test_*.py' -v
    python3 -m unittest discover -s guest/ssh -p 'test_*.py' -v
    python3 -m unittest discover -s scripts/acceptance -p 'test_*.py' -v
    python3 -m unittest discover -s scripts/gateway -p 'test_*.py' -v
    python3 -m unittest discover -s tests -p 'test_*.py' -v

# Both adapters use only temporary fixtures, including the live-dispatch test.
test-shell:
    bash scripts/acceptance/opencode-test.sh
    bash scripts/acceptance/opencode-live-adapter-test.sh

check-shell:
    git ls-files -z -- '*.sh' | xargs -0 -r -n1 bash -n

check-nix:
    nix --quiet flake check --no-build --no-update-lock-file
    nix --quiet flake check --no-update-lock-file

modules:
    go mod verify

# Offline local targets and heading fragments in all tracked Markdown.
check-markdown-links:
    python3 scripts/check_markdown_links.py

# Integration tests run only on disposable Incus. By default each test
# deletes its uniquely named project (including test containers and volumes).
# --retain preserves projects for inspection; --unsafe provides opt-in flags.
integration-test *args:
    @set -euo pipefail; flags='{{ args }}'; export AGINCTUS_INCUS_RETAIN=0; for flag in $flags; do case "$flag" in --unsafe) export AGINCTUS_INCUS_INTEGRATION=1 AGINCTUS_INCUS_DISPOSABLE=YES ;; --retain) export AGINCTUS_INCUS_RETAIN=1 ;; *) echo "Unknown integration-test option: $flag (expected --unsafe or --retain)" >&2; exit 2 ;; esac; done; if [[ "${AGINCTUS_INCUS_INTEGRATION:-}" != "1" || "${AGINCTUS_INCUS_DISPOSABLE:-}" != "YES" ]]; then echo "Use 'just integration-test --unsafe' on a disposable host, or set AGINCTUS_INCUS_INTEGRATION=1 and AGINCTUS_INCUS_DISPOSABLE=YES" >&2; exit 1; fi; go test ./internal/orchestration/drivers/compose -run '^TestIncusCompose.*Integration$' -count=1 -v -timeout 12m
