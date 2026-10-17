#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/../.."
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
adapter="$PWD/scripts/acceptance/opencode-live-adapter.sh"
id=aginctus-acceptance.ABCD1234
ws="$tmp/$id/workspace"
mkdir -p "$ws" "$tmp/hooks"
export AGINCTUS_ACCEPTANCE_LIVE=I_UNDERSTAND
export AGINCTUS_ACCEPTANCE_LIVE_HOOKS="$tmp/hooks"
if bash "$adapter" preflight "$ws" "$id" >/dev/null 2>&1; then
  echo "incomplete hooks must fail" >&2; exit 1
fi
for phase in preflight open start prompt wait credential-check gateway-evidence stop close; do
  cat >"$tmp/hooks/$phase" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$*" >>"$ACCEPTANCE_HOOK_LOG"
EOF
  chmod +x "$tmp/hooks/$phase"
done
export ACCEPTANCE_HOOK_LOG="$tmp/seen"
bash "$adapter" prompt "$ws" "$id" 'inspect and fix Add'
grep -Fq 'inspect and fix Add' "$tmp/seen"
if bash "$adapter" preflight "$ws" "not-our-resource" >/dev/null 2>&1; then
  echo "unsafe identity accepted" >&2; exit 1
fi
if bash "$adapter" prompt "$ws" "$id" >/dev/null 2>&1; then
  echo "missing prompt accepted" >&2; exit 1
fi
unset AGINCTUS_ACCEPTANCE_LIVE
if bash "$adapter" stop "$ws" "$id" >/dev/null 2>&1; then
  echo "missing live opt-in accepted" >&2; exit 1
fi
echo "PASS: live adapter dispatch, input validation and fail-closed setup"
