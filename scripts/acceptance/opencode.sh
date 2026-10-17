#!/usr/bin/env bash
# Issue #124: isolated OpenCode acceptance orchestration. See docs/acceptance-opencode.md.
set -Eeuo pipefail
umask 077

mode="${1:-}"
case "$mode" in mock|live) ;; *) echo "usage: bash $0 mock|live" >&2; exit 2 ;; esac
: "${AGINCTUS_ACCEPTANCE_ADAPTER:?set AGINCTUS_ACCEPTANCE_ADAPTER to an executable adapter}"
if [[ ! -f "$AGINCTUS_ACCEPTANCE_ADAPTER" || ! -x "$AGINCTUS_ACCEPTANCE_ADAPTER" ]]; then
  echo "acceptance adapter must be an executable file" >&2; exit 2
fi
if [[ "$mode" == live && "${AGINCTUS_ACCEPTANCE_LIVE:-}" != "I_UNDERSTAND" ]]; then
  echo "live mode requires AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND" >&2; exit 2
fi

root="$(mktemp -d "${TMPDIR:-/tmp}/aginctus-acceptance.XXXXXXXX")"
workspace="$root/workspace"
mkdir -p "$workspace"
# Run identity is never taken from caller input, and no pre-existing workspace is touched.
run_id="$(basename "$root")"
report="$root/report.txt"
started=0
opened=0
success=0

cleanup() {
  local status=$?
  trap - EXIT
  if (( started )); then
    "$AGINCTUS_ACCEPTANCE_ADAPTER" stop "$workspace" "$run_id" >/dev/null 2>&1 || echo "WARN: agent stop failed ($run_id)" >&2
  fi
  if (( opened )); then
    "$AGINCTUS_ACCEPTANCE_ADAPTER" close "$workspace" "$run_id" >/dev/null 2>&1 || echo "WARN: workspace close failed ($run_id)" >&2
  fi
  if (( success )); then
    echo "PASS: $mode acceptance ($run_id)"
  else
    echo "FAIL: $mode acceptance ($run_id), exit $status" >&2
  fi
  # Never emit adapter output: it may contain credentials. Reports contain only fixed labels.
  rm -rf -- "$root"
  exit "$status"
}
trap cleanup EXIT

cat > "$workspace/go.mod" <<'EOF'
module example.invalid/aginctus-acceptance
go 1.22
EOF
cat > "$workspace/calc.go" <<'EOF'
package calc

func Add(a, b int) int { return a - b }
EOF
cat > "$workspace/calc_test.go" <<'EOF'
package calc

import "testing"

func TestAdd(t *testing.T) {
 if got := Add(2, 3); got != 5 { t.Fatalf("Add(2,3) = %d; want 5", got) }
}
EOF

# Private, disposable evidence: the adapter must independently correlate a model
# request to this unique run ID; printing a nonce alone is NOT live gateway proof.
nonce="$run_id"
prompt="Inspect calc.go and calc_test.go in this workspace. Fix Add so that go test ./... passes. Run go test ./... and report the result. Acceptance correlation ID: $nonce"
if [[ "$mode" == live ]]; then
  "$AGINCTUS_ACCEPTANCE_ADAPTER" preflight "$workspace" "$nonce" >"$root/preflight.out" 2>&1 || { echo "FAIL: live preflight" >&2; exit 1; }
fi
"$AGINCTUS_ACCEPTANCE_ADAPTER" open "$workspace" "$nonce" >"$root/open.out" 2>&1 || { echo "FAIL: opening workspace" >&2; exit 1; }
opened=1
# Mark before invoking start to allow cleanup after a partial start.
started=1
"$AGINCTUS_ACCEPTANCE_ADAPTER" start "$workspace" "$nonce" >"$root/start.out" 2>&1 || { echo "FAIL: starting agent" >&2; exit 1; }
"$AGINCTUS_ACCEPTANCE_ADAPTER" prompt "$workspace" "$nonce" "$prompt" >"$root/prompt.out" 2>&1 || { echo "FAIL: submitting task" >&2; exit 1; }
"$AGINCTUS_ACCEPTANCE_ADAPTER" wait "$workspace" "$nonce" >"$root/wait.out" 2>&1 || { echo "FAIL: agent completion" >&2; exit 1; }

# Compare exact expected source change, then execute the actual test, not a mock.
if ! grep -Eq 'return a[[:space:]]*\+[[:space:]]*b' "$workspace/calc.go"; then
  echo "FAIL: expected source modification missing" >&2; exit 1
fi
(cd "$workspace" && go test ./...) >"$root/tests.out" 2>&1 || { echo "FAIL: workspace tests" >&2; exit 1; }
"$AGINCTUS_ACCEPTANCE_ADAPTER" credential-check "$workspace" "$nonce" >"$root/creds.out" 2>&1 || { echo "FAIL: workload credential isolation" >&2; exit 1; }
"$AGINCTUS_ACCEPTANCE_ADAPTER" gateway-evidence "$workspace" "$nonce" >"$root/gateway.out" 2>&1 || { echo "FAIL: gateway evidence" >&2; exit 1; }
# The adapter MUST output a single signed-off indicator only after inspecting
# gateway-side records, not workload self-reporting. Mock adapter supplies synthetic evidence.
if ! grep -Fxq "gateway-observed:$nonce" "$root/gateway.out"; then
  echo "FAIL: gateway correlation absent" >&2; exit 1
fi
if [[ "$mode" == live ]]; then
  # Explicit provider-side completion proof, issued by the live adapter only.
  if ! grep -Fxq "upstream-inference-confirmed:$nonce" "$root/gateway.out"; then
    echo "FAIL: upstream inference not proven" >&2; exit 1
  fi
fi
success=1
