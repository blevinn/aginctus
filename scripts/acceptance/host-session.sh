#!/usr/bin/env bash
# Manual host acceptance: deliberately bounded local-only preflight/worksheet.
set -euo pipefail
umask 077

usage() { printf 'usage: bash scripts/acceptance/host-session.sh preflight|init\n' >&2; exit 2; }
[[ $# == 1 ]] || usage
case "$1" in
  preflight)
    printf 'Host acceptance preflight (LOCAL ONLY; no Incus/network queries)\n'
    for tool in git nix just aginctus incus; do
      if command -v "$tool" >/dev/null 2>&1; then
        printf '%s: available\n' "$tool"
      else
        printf '%s: missing (review prerequisite)\n' "$tool"
      fi
    done
    if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      printf 'git revision: %s\n' "$(git rev-parse --verify HEAD)"
      if [[ -z "$(git status --porcelain)" ]]; then
        printf 'git worktree: clean\n'
      else
        printf 'git worktree: modified (review before proceeding)\n'
      fi
    else
      printf 'git worktree: unavailable\n'
    fi
    printf 'No host mutation, Incus query or inference was attempted.\n'
    ;;
  init)
    # Existing, private XDG state location. No paths supplied by an agent or
    # implicit writes under the repository; do not follow caller-chosen roots.
    root="${XDG_STATE_HOME:-$HOME/.local/state}/aginctus"
    if [[ ! -d "$root" || -L "$root" || -L "$(dirname "$root")" ]]; then
      printf 'Provide an existing non-symlinked, private state directory first.\n' >&2; exit 1
    fi
    if [[ "$(stat -c %u -- "$root")" != "$(id -u)" || "$(stat -c %a -- "$root")" != 700 ]]; then
      printf 'State directory must be operator-owned with mode 0700.\n' >&2; exit 1
    fi
    run_dir="$(mktemp -d "$root/manual-host-acceptance.XXXXXXXX")"
    report="$run_dir/worksheet.md"
    revision="unavailable"
    if git rev-parse --verify HEAD >/dev/null 2>&1; then revision="$(git rev-parse HEAD)"; fi
    printf '%s\n\nRevision: %s%s%s\n\n' '# Aginctus manual host acceptance' '`' "$revision" '`' > "$report"
    for phase in baseline first-contact topology provisioning coding configuration recovery reporting cleanup; do
      printf '## %s\n\nStatus: not_run\n\nExpected:\n\nObserved:\n\nFriction:\n\nEvidence (redacted references only):\n\n' "$phase" >> "$report"
    done
    printf '## Findings and release disposition\n\nNo signoff inferred from this worksheet.\n' >> "$report"
    chmod 600 "$report"
    printf 'Private worksheet created at: %s\n' "$report"
    printf 'Review directory ownership/permissions and do not share unredacted contents.\n'
    ;;
  *) usage ;;
esac
