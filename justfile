set shell := ["bash", "-euo", "pipefail", "-c"]

mod build '.just/build.just'
mod update '.just/update.just'
mod fmt '.just/fmt.just'
mod check '.just/check.just'

# Compatibility entry points; new work should use 'just check <recipe>'.
test:
    just check test

vet:
    just check vet

modules:
    just check modules

check-all:
    just check all

check-lint:
    just check lint

check-go-lint:
    just check go-lint

check-python-lint:
    just check python-lint

check-shell-lint:
    just check shell-lint

test-python:
    just check python

test-shell:
    just check shell-tests

check-shell:
    just check shell

check-nix:
    just check nix

check-markdown-links:
    just check markdown

# Run the heavyweight NixOS firewall VM tests explicitly (x86_64-linux).
test-nix-firewall:
    nix build -L --no-link .#firewall-iptables-integration .#firewall-firewalld-integration .#firewall-nixos-nft-integration

# Builds and runs a disposable QEMU VM with real Incus and the packaged CLI.
# Unlike 'just test', this is a heavyweight integration check.
test-nix *args:
    bash tests/nixos-incus-vm.sh {{ args }}

# Integration tests run only on disposable Incus. By default each test
# deletes its uniquely named project (including test containers and volumes).
# --retain preserves projects for inspection; --unsafe provides opt-in flags.
integration-test *args:
    @set -euo pipefail; flags='{{ args }}'; export AGINCTUS_INCUS_RETAIN=0; for flag in $flags; do case "$flag" in --unsafe) export AGINCTUS_INCUS_INTEGRATION=1 AGINCTUS_INCUS_DISPOSABLE=YES ;; --retain) export AGINCTUS_INCUS_RETAIN=1 ;; *) echo "Unknown integration-test option: $flag (expected --unsafe or --retain)" >&2; exit 2 ;; esac; done; if [[ "${AGINCTUS_INCUS_INTEGRATION:-}" != "1" || "${AGINCTUS_INCUS_DISPOSABLE:-}" != "YES" ]]; then echo "Use 'just integration-test --unsafe' on a disposable host, or set AGINCTUS_INCUS_INTEGRATION=1 and AGINCTUS_INCUS_DISPOSABLE=YES" >&2; exit 1; fi; go test ./internal/orchestration/drivers/compose -run '^TestIncusCompose.*Integration$' -count=1 -v -timeout 12m
