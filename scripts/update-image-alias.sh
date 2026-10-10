#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <alias> <metadata.tar.xz> <rootfs.squashfs>" >&2
  exit 2
fi

alias_name="$1"
metadata="$2"
rootfs="$3"
stage="${alias_name}-staged-$$"
backup="${alias_name}-previous-$$"
old_moved=0
promoted=0

cleanup() {
  local status=$?
  trap - EXIT
  if (( status != 0 && old_moved && !promoted )); then
    if ! incus image alias rename "$backup" "$alias_name"; then
      echo "ERROR: rollback failed; previous image remains under $backup" >&2
    fi
  fi
  incus image alias delete "$stage" >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

# Do not disturb the live alias until the complete import succeeds.
incus image import "$metadata" "$rootfs" --alias "$stage"
if incus image alias list --format csv -c n | grep -Fxq "$alias_name"; then
  incus image alias rename "$alias_name" "$backup"
  old_moved=1
fi

incus image alias rename "$stage" "$alias_name"
promoted=1
if (( old_moved )); then
  incus image alias delete "$backup"
fi
