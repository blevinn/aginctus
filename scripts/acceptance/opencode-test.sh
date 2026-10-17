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
 stop) touch "${ACCEPTANCE_TEST_EVENTS}/stop" ;;
 close) touch "${ACCEPTANCE_TEST_EVENTS}/close" ;;
 *) exit 1 ;;
esac
EOF
chmod +x "$tmp/adapter"
mkdir "$tmp/events"
export ACCEPTANCE_TEST_EVENTS="$tmp/events"
export AGINCTUS_ACCEPTANCE_ADAPTER="$tmp/adapter"
bash scripts/acceptance/opencode.sh mock
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
echo "PASS: deterministic acceptance orchestration and cleanup"
