#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/../.."
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
cat >"$tmp/adapter" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
action="$1"; workspace="$2"; id="$3"
case "$action" in
 open) touch "$workspace/.opened" ;;
 start) test -f "$workspace/.opened"; touch "$workspace/.started" ;;
 prompt)
   test -f "$workspace/.started"
   test "$#" -eq 4
   grep -Fq "$id" <<<"$4"
   ;;
 wait)
   test -f "$workspace/.started"
   if [[ "${ACCEPTANCE_TEST_FAIL:-}" == 1 ]]; then exit 7; fi
   sed -i 's/return a - b/return a + b/' "$workspace/calc.go"
   ;;
 credential-check) test ! -f "$workspace/upstream-token" ;;
 gateway-evidence) printf 'gateway-observed:%s\n' "$id" ;;
 stop)
   touch "${ACCEPTANCE_TEST_EVENTS}/stop"
   if [[ "${ACCEPTANCE_TEST_CLEANUP_FAIL:-}" == 1 ]]; then exit 9; fi
   ;;
 close) touch "${ACCEPTANCE_TEST_EVENTS}/close" ;;
 *) exit 1 ;;
esac
EOF
chmod +x "$tmp/adapter"
mkdir "$tmp/events"
export ACCEPTANCE_TEST_EVENTS="$tmp/events"
export AGINCTUS_ACCEPTANCE_ADAPTER="$tmp/adapter"
mkdir -m 700 "$tmp/reports"
export AGINCTUS_ACCEPTANCE_REPORT_ROOT="$tmp/reports"
bash scripts/acceptance/opencode.sh mock
python3 - "$tmp/reports" <<'PY'
import json
import sys
from pathlib import Path

reports = list(Path(sys.argv[1]).glob("*/result.json"))
assert len(reports) == 1
report = json.loads(reports[0].read_text())
assert report["profile"] == "local_acceptance"
assert report["mode"] == "mock"
assert report["status"] == "incomplete"
assert report["harness_exit_code"] == 0
assert [entry["status"] for entry in report["results"]] == [
    "pass", "pass", "not_run", "not_run", "not_run", "not_run",
]
assert report["cleanup_commands"] == {"stop": "pass", "close": "pass"}
assert report["upstream_inference"] == "not_verified"
assert report["complete_acceptance"] is False
PY
test -f "$tmp/events/stop"
test -f "$tmp/events/close"
rm "$tmp/events/"*
# Failure must still stop and close; live opt-in must reject without opening.
export ACCEPTANCE_TEST_FAIL=1
if bash scripts/acceptance/opencode.sh mock; then echo "unexpected mock pass" >&2; exit 1; fi
test -f "$tmp/events/stop"
test -f "$tmp/events/close"
rm "$tmp/events/"*
if bash scripts/acceptance/opencode.sh live; then echo "unexpected live pass" >&2; exit 1; fi
test ! -e "$tmp/events/stop"
test ! -e "$tmp/events/close"

# A cleanup command failure must fail the harness, even when fixture tests pass.
unset ACCEPTANCE_TEST_FAIL
export ACCEPTANCE_TEST_CLEANUP_FAIL=1
if bash scripts/acceptance/opencode.sh mock; then echo "unexpected cleanup pass" >&2; exit 1; fi
python3 - "$tmp/reports" <<'PY'
import json
import sys
from pathlib import Path

reports = [json.loads(path.read_text()) for path in Path(sys.argv[1]).glob("*/result.json")]
assert len(reports) == 3
assert sorted(report["harness_exit_code"] for report in reports) == [0, 1, 1]
cleanup_failure = [report for report in reports if report["cleanup_commands"]["stop"] == "fail"]
assert len(cleanup_failure) == 1
assert cleanup_failure[0]["status"] == "fail"
assert cleanup_failure[0]["results"][0]["status"] == "pass"
assert cleanup_failure[0]["results"][1]["status"] == "pass"
assert cleanup_failure[0]["cleanup"] == "not_verified"
PY
unset ACCEPTANCE_TEST_CLEANUP_FAIL
rm "$tmp/events/"*
mkdir -m 755 "$tmp/unsafe-results"
chmod 755 "$tmp/unsafe-results"
export AGINCTUS_ACCEPTANCE_REPORT_ROOT="$tmp/unsafe-results"
if bash scripts/acceptance/opencode.sh mock; then
  echo "unsafe evidence root unexpectedly accepted" >&2; exit 1
fi
test ! -e "$tmp/events/stop"
test ! -e "$tmp/events/close"
unset AGINCTUS_ACCEPTANCE_REPORT_ROOT
echo "PASS: deterministic acceptance orchestration and cleanup"
