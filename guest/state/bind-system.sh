#!/usr/bin/env bash
# Bind durable, Incus-attached system state into the Herdr client's runtime
# paths. This runs only inside the guest, before herdr.service starts.
set -euo pipefail

source_root=/var/lib/aginctus/state/system

fail() {
  printf 'Aginctus system state: %s\n' "$*" >&2
  exit 1
}

mountpoint -q -- "$source_root" || fail "required Incus state-system volume is not mounted"

[[ -d "$source_root" && ! -L "$source_root" ]] || fail "unsafe system-state mount"
[[ "$(stat -c '%u' "$source_root")" == 0 ]] || fail "system-state root must be owned by root"

# Every runtime destination is explicitly approved; no arbitrary paths are
# accepted from user configuration or from the volume.
for item in herdr ssh; do
  source_dir="$source_root/$item"
  case "$item" in
    herdr) target=/var/lib/herdr ;;
    ssh) target=/var/lib/aginctus/ssh ;;
  esac
  [[ ! -L "$source_dir" && ! -L "$target" ]] || fail "symlink at managed state path"
  [[ ! -e "$source_dir" || -d "$source_dir" ]] || fail "non-directory system-state source"
  [[ ! -e "$target" || -d "$target" ]] || fail "non-directory bind destination"
  mkdir -p -- "$source_dir" "$target"
  chmod 0700 -- "$source_dir"
  [[ "$(stat -c '%u' "$source_dir")" == 0 ]] || fail "state directory must be root-owned"
  if mountpoint -q -- "$target"; then
    fail "destination already mounted (refusing to replace mount)"
  fi
  if [[ -n "$(find "$target" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    fail "destination contains rootfs state; explicit migration required"
  fi
  mount --bind -- "$source_dir" "$target"
done
