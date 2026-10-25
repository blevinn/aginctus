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

# An operator may explicitly opt into a private persistent report. Validate the
# destination BEFORE any adapter / provider work; never create it implicitly.
reporter="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/local_report.py"
if [[ -n "${AGINCTUS_ACCEPTANCE_REPORT_ROOT:-}" ]]; then
  python3 "$reporter" --check-root "$AGINCTUS_ACCEPTANCE_REPORT_ROOT" || exit 1
fi

root="$(mktemp -d "${TMPDIR:-/tmp}/aginctus-acceptance.XXXXXXXX")"
workspace="$root/workspace"
mkdir -p "$workspace"
# Run identity is never taken from caller input, and no pre-existing workspace is touched.
run_id="$(basename "$root")"
started=0
opened=0
success=0
fixture_status=not_run
tests_status=not_run
stop_status=not_run
close_status=not_run
local_workspace_removed=not_run

cleanup() {
  local status=$?
  trap - EXIT
  if (( started )); then
    if "$AGINCTUS_ACCEPTANCE_ADAPTER" stop "$workspace" "$run_id" >/dev/null 2>&1; then
      stop_status=pass
    else
      stop_status=fail
      status=1
      echo "WARN: agent stop failed ($run_id)" >&2
    fi
  fi
  if (( opened )); then
    if "$AGINCTUS_ACCEPTANCE_ADAPTER" close "$workspace" "$run_id" >/dev/null 2>&1; then
      close_status=pass
    else
      close_status=fail
      status=1
      echo "WARN: workspace close failed ($run_id)" >&2
    fi
  fi
  # Inspect the runner-owned disposable root *after* removing it, not just
  # the rm exit code. This never proves remote Herdr/Incus resources vanished.
  # Suppress cleanup diagnostics because guest-writable paths may be sensitive.
  if rm -rf -- "$root" >/dev/null 2>&1 && [[ ! -e "$root" && ! -L "$root" ]]; then
    local_workspace_removed=pass
  else
    local_workspace_removed=fail
    status=1
    echo "WARN: disposable local workspace removal not confirmed ($run_id)" >&2
  fi
  # The host runner knows its own fixture/test, cleanup command and local
  # root-removal outcomes. None establishes upstream inference or remote cleanup.
  if [[ -n "${AGINCTUS_ACCEPTANCE_REPORT_ROOT:-}" ]]; then
    if ! python3 "$reporter" --output-root "$AGINCTUS_ACCEPTANCE_REPORT_ROOT" \
      --run-id "$run_id" --mode "$mode" --exit-code "$status" \
      --fixture "$fixture_status" --tests "$tests_status" \
      --stop "$stop_status" --close "$close_status" \
      --local-workspace-removed "$local_workspace_removed"; then
      status=1
      echo "ERROR: acceptance evidence recording failed" >&2
    fi
  fi
  if (( success && status == 0 )); then
    echo "PASS: $mode acceptance ($run_id)"
  else
    echo "FAIL: $mode acceptance ($run_id), exit $status" >&2
  fi
  # Never emit adapter output: it may contain credentials. The disposable
  # root was removed and independently checked before writing the report.
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
  fixture_status=fail
  echo "FAIL: expected source modification missing" >&2; exit 1
fi
fixture_status=pass
if ! (cd "$workspace" && go test ./...) >"$root/tests.out" 2>&1; then
  tests_status=fail
  echo "FAIL: workspace tests" >&2; exit 1
fi
tests_status=pass
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
