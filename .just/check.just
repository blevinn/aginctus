# Just modules do not inherit the root justfile shell settings. Keep
# pipefail here so git ls-files errors cannot be masked by xargs.
set shell := ["bash", "-euo", "pipefail", "-c"]

# Check recipes execute at the project root despite living under .just/.
# 'ci' and 'all' never opt in to disposable Incus or billable acceptance.

[default]
[no-cd]
default: go

# Fast local baseline: Go module integrity, tests, and vet.
[no-cd]
go: modules test vet

[no-cd]
modules:
    go mod verify

[no-cd]
test:
    @if [[ "${AGINCTUS_CHECK_STRUCTURED:-0}" == 1 ]]; then go test -json ./...; else go test ./...; fi

[no-cd]
vet:
    go vet ./...

# Standard hermetic validation uses the same concise reporter as manual
# runs. Reporter hardens the environment independently for each suite and
# records failures across all eight safe suites (not just the first failure).
[no-cd]
ci: report

# Same safe comprehensive suite for humans; VM/Incus/live tests stay opt-in.
[no-cd]
all: ci

# The linters use pinned development-shell tools and their own config.
[no-cd]
lint: go-lint python-lint shell-lint

[no-cd]
go-lint:
    golangci-lint run

[no-cd]
python-lint:
    ruff check .

[no-cd]
shell-lint:
    git ls-files -z -- '*.sh' | xargs -0 -r -n1 shellcheck

[no-cd]
python:
    python3 -m unittest discover -s scripts/vm_harness -p 'test_*.py' -v
    python3 -m unittest discover -s guest/opencode -p 'test_*.py' -v
    python3 -m unittest discover -s guest/ssh -p 'test_*.py' -v
    python3 -m unittest discover -s scripts/acceptance -p 'test_*.py' -v
    python3 -m unittest discover -s scripts/gateway -p 'test_*.py' -v
    python3 -m unittest discover -s tests -p 'test_*.py' -v

# Both adapters use temporary fixtures; do not run live acceptance.
[no-cd]
shell-tests:
    bash scripts/acceptance/opencode-test.sh
    bash scripts/acceptance/opencode-live-adapter-test.sh

[no-cd]
shell:
    git ls-files -z -- '*.sh' | xargs -0 -r -n1 bash -n

[no-cd]
nix:
    nix --quiet flake check --no-build --no-update-lock-file
    nix --quiet flake check --no-update-lock-file

# Offline local targets and heading fragments in tracked Markdown.
[no-cd]
markdown:
    python3 scripts/check_markdown_links.py

# Only validates shell commands in canonical docs against CLI help.
# Never executes any command copied from a document.
[no-cd]
docs-examples:
    go run ./cmd/aginctus help | python3 scripts/check_docs_examples.py


# Standard concise suite reporting. The @ prefix suppresses Just's recipe
# echo so JSON mode emits exactly one machine-readable object.
[no-cd]
report:
    @python3 scripts/checks/report.py --format text

[no-cd]
report-json:
    @python3 scripts/checks/report.py --format json

# Read-only, explicit VM evidence inspection. Never runs or discovers VMs.
# quote() prevents the operator-provided run ID becoming shell syntax.
[no-cd]
vm-report run_id:
    @python3 scripts/checks/vm_evidence.py --format text {{quote(run_id)}}

[no-cd]
vm-report-json run_id:
    @python3 scripts/checks/vm_evidence.py --format json {{quote(run_id)}}

# Operator-only, explicit read-only inspection of the configured pilot.
# Not a dependency of ci/all; never runs a live acceptance or model request.
[no-cd]
preflight-report check:
    @python3 scripts/checks/preflight_report.py --format text {{quote(check)}}

[no-cd]
preflight-report-json check:
    @python3 scripts/checks/preflight_report.py --format json {{quote(check)}}

# Read-only composition of an explicitly supplied prior hermetic JSON report.
# This never runs checks or imports live acceptance as proof of inference.
[no-cd]
coverage hermetic_json:
    @python3 -m scripts.checks.coverage --format text --hermetic-json {{quote(hermetic_json)}}

[no-cd]
coverage-json hermetic_json:
    @python3 -m scripts.checks.coverage --format json --hermetic-json {{quote(hermetic_json)}}
