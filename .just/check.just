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
    go test ./...

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
