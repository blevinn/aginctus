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

check-docs-examples:
    just check docs-examples

# Run the heavyweight NixOS firewall VM tests explicitly (x86_64-linux).
test-nix-firewall:
    nix build -L --no-link .#firewall-iptables-integration .#firewall-firewalld-integration .#firewall-nixos-nft-integration

# Boot a pinned real Incus guest using the shared VM fixture (#427).
# Always explicit: excluded from check-all and normal CI.
test-vm-smoke:
    python3 tests/vm_harness/runner.py test smoke

# Guest-only Btrfs Incus pool and real Aginctus management bridge checks.
test-vm-storage-network:
    python3 tests/vm_harness/runner.py test storage-network

# Real packaged Herdr/OpenCode images and Incus lifecycle, no OCI inference.
test-vm-runtime-lifecycle:
    python3 tests/vm_harness/runner.py test runtime-lifecycle

# Fixed managed fixtures: reviewed source and an independently approved local
# x86_64 KVM builder only. Request sandboxing; the operator must verify the
# daemon's effective policy. No host Python driver or arbitrary target input.
# These are experimental, not defaults or proof of fresh execution.
testenv-worker-lifecycle:
    env -u NIX_CONFIG -u NIX_REMOTE -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK -u INCUS_SOCKET -u INCUS_DIR nix build --no-link --no-update-lock-file --option sandbox true --option builders "" --print-out-paths .#testenv-disposable-worker-fixture

testenv-managed-native-instance:
    env -u NIX_CONFIG -u NIX_REMOTE -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK -u INCUS_SOCKET -u INCUS_DIR nix build --no-link --no-update-lock-file --option sandbox true --option builders "" --print-out-paths .#testenv-managed-native-instance

testenv-payload-transfer:
    env -u NIX_CONFIG -u NIX_REMOTE -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK -u INCUS_SOCKET -u INCUS_DIR nix build --no-link --no-update-lock-file --option sandbox true --option builders "" --print-out-paths .#testenv-payload-transfer

testenv-nested-kvm:
    env -u NIX_CONFIG -u NIX_REMOTE -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK -u INCUS_SOCKET -u INCUS_DIR nix build --no-link --no-update-lock-file --option sandbox true --option builders "" --print-out-paths .#testenv-nested-kvm

testenv-project-admission:
    env -u NIX_CONFIG -u NIX_REMOTE -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK -u INCUS_SOCKET -u INCUS_DIR nix build --no-link --no-update-lock-file --option sandbox true --option builders "" --print-out-paths .#testenv-project-admission

testenv-runtime-resources:
    env -u NIX_CONFIG -u NIX_REMOTE -u QEMU_OPTS -u QEMU_KERNEL_PARAMS -u SSH_AUTH_SOCK -u INCUS_SOCKET -u INCUS_DIR nix build --no-link --no-update-lock-file --option sandbox true --option builders "" --print-out-paths .#testenv-runtime-resources

# Native scoped instance lifecycle and dry-run against disposable Incus.
test-vm-native-instance:
    python3 tests/vm_harness/runner.py test native-instance

# Offline native lifecycle fixtures; execute only on a dedicated VM worker.
test-vm-native-gateway-stopped-pair:
    python3 tests/vm_harness/runner.py test native-gateway-stopped-pair

test-vm-native-gateway-stop-retention:
    python3 tests/vm_harness/runner.py test native-gateway-stop-retention

test-vm-native-joined-lifecycle:
    python3 tests/vm_harness/runner.py test native-joined-lifecycle

# Gate C project/image lease and collision
test-vm-acceptance-project-image:
    python3 tests/vm_harness/runner.py test acceptance-project-image

# Gate D nftables policy
test-vm-acceptance-nft:
    python3 tests/vm_harness/runner.py test acceptance-nft

# Gate D dormant deny
test-vm-acceptance-egress-denial:
    python3 tests/vm_harness/runner.py test acceptance-egress-denial

# Gate D firewalld grant/revoke
test-vm-acceptance-firewalld:
    python3 tests/vm_harness/runner.py test acceptance-firewalld

# Gate E: guest-local multi-environment state and public CLI authority boundaries.
test-vm-acceptance-state-boundary:
    python3 tests/vm_harness/runner.py test acceptance-state-boundary

# Real LiteLLM/PostgreSQL OCI and Herdr/OpenCode topology (no model calls).
# May need OCI registry access; use a disposable NixOS KVM worker only.
test-vm-gateway-topology:
    python3 tests/vm_harness/runner.py test gateway-topology
test-vm-synthetic-gateway:
    python3 tests/vm_harness/runner.py test synthetic-gateway
# Real LiteLLM function-call continuation transport with FAKE tool outputs.
# Operator-only KVM, no Herdr/OpenCode tool execution or live inference.
test-vm-synthetic-tool-gateway:
    python3 tests/vm_harness/runner.py test synthetic-tool-gateway
# Real Herdr/OpenCode tools through the synthetic gateway, dedicated worker only.
test-vm-full-stack:
    python3 tests/vm_harness/runner.py test full-stack
# Genuine pinned OpenCode tool execution INSIDE a disposable no-egress VM.
# Still a loopback synthetic model: not Herdr/LiteLLM acceptance.
test-vm-opencode-tool-loopback:
    python3 tests/vm_harness/runner.py test opencode-tool-loopback

# Real Incus foreign-resource and daemon-outage recovery assertions.
test-vm-ownership-recovery:
    python3 tests/vm_harness/runner.py test ownership-recovery

# Heavyweight vmTests are explicit NixOS builds (never default CI or host Incus).
# Project authority includes the restricted TLS peer trust lookup and revocation.
test-vm-acceptance-project-authority:
    python3 tests/vm_harness/runner.py test acceptance-project-authority

test-vm-acceptance-environment-state:
    python3 tests/vm_harness/runner.py test acceptance-environment-state

test-vm-acceptance-accepted-state:
    python3 tests/vm_harness/runner.py test acceptance-accepted-state

test-vm-acceptance-home-manager:
    python3 tests/vm_harness/runner.py test acceptance-home-manager

# Inspect the same VM fixture with the interactive NixOS test driver.
# Run under a dedicated unprivileged worker identity, never on pilot host.
vm-debug:
    python3 tests/vm_harness/runner.py debug smoke

# Hostless safety tests for runner command/environment selection.
test-vm-runner:
    python3 -m unittest discover -s tests/vm_harness -p 'test_*.py' -v

# Builds and runs a disposable QEMU VM with real Incus and the packaged CLI.
# Unlike 'just test', this is a heavyweight integration check.
test-nix *args:
    bash tests/nixos-incus-vm.sh {{ args }}

# Integration tests run only on disposable Incus. By default each test
# deletes its uniquely named project (including test containers and volumes).
# --retain preserves projects for inspection; --unsafe provides opt-in flags.
integration-test *args:
    @set -euo pipefail; flags='{{ args }}'; export AGINCTUS_INCUS_RETAIN=0; for flag in $flags; do case "$flag" in --unsafe) export AGINCTUS_INCUS_INTEGRATION=1 AGINCTUS_INCUS_DISPOSABLE=YES ;; --retain) export AGINCTUS_INCUS_RETAIN=1 ;; *) echo "Unknown integration-test option: $flag (expected --unsafe or --retain)" >&2; exit 2 ;; esac; done; if [[ "${AGINCTUS_INCUS_INTEGRATION:-}" != "1" || "${AGINCTUS_INCUS_DISPOSABLE:-}" != "YES" ]]; then echo "Use 'just integration-test --unsafe' on a disposable host, or set AGINCTUS_INCUS_INTEGRATION=1 and AGINCTUS_INCUS_DISPOSABLE=YES" >&2; exit 1; fi; go test ./internal/orchestration/drivers/compose -run '^TestIncusCompose.*Integration$' -count=1 -v -timeout 12m
