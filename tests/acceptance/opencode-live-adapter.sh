#!/usr/bin/env bash
# Fail-closed phase dispatcher for the opt-in live acceptance harness.
# Integration commands are supplied by the operator, not guessed from Herdr.
set -Eeuo pipefail
umask 077
phase="${1:-}"
case "$phase" in
  preflight|open|start|prompt|wait|credential-check|gateway-evidence|stop|close) ;;
  *) echo "unknown live acceptance phase" >&2; exit 2 ;;
esac
if [[ "${AGINCTUS_ACCEPTANCE_LIVE:-}" != I_UNDERSTAND ]]; then
  echo "live acceptance not authorized" >&2
  exit 2
fi
if (( $# < 3 )); then echo "missing workspace or run identity" >&2; exit 2; fi
workspace="$2"
run_id="$3"
if [[ ! "$run_id" =~ ^aginctus-acceptance\.[[:alnum:]]+$ ||
      "$(basename "$(dirname "$workspace")")" != "$run_id" ||
      "$(basename "$workspace")" != workspace ]]; then
  echo "invalid disposable acceptance identity" >&2; exit 2
fi
if [[ "$phase" == prompt && $# -ne 4 || "$phase" != prompt && $# -ne 3 ]]; then
  echo "invalid phase arguments" >&2; exit 2
fi
hooks="${AGINCTUS_ACCEPTANCE_LIVE_HOOKS:-}"
if [[ -z "$hooks" || ! -d "$hooks" || "$hooks" != /* ]]; then
  echo "set AGINCTUS_ACCEPTANCE_LIVE_HOOKS to an absolute directory" >&2; exit 2
fi
# Require every phase before any mutation; even cleanup must use the same contract.
for required in preflight open start prompt wait credential-check gateway-evidence stop close; do
  if [[ ! -f "$hooks/$required" || ! -x "$hooks/$required" || -L "$hooks/$required" ]]; then
    echo "missing reviewed live phase: $required" >&2; exit 2
  fi
done
# Hooks MUST perform independent checks. Never infer success from an empty command.
exec "$hooks/$phase" "${@:2}"
