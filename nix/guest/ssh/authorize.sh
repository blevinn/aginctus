#!/usr/bin/env bash
set -euo pipefail

# Guest-side helper installed only in managed workload images.
#
# Aginctus invokes this helper through authenticated Incus guest exec. It is the
# only code allowed to mutate Aginctus-managed SSH authorization in a workload.
# It never reads the Herdr client's private key; it receives public keys on stdin.
#
# The matching producer is aginctus-ssh-client-identity in the Herdr client:
#
#   Herdr client guest:
#     aginctus-ssh-client-identity ensure
#       -> public_key<TAB>ssh-ed25519 ... aginctus:<generation>
#
#   Aginctus host process:
#     extracts that public key and sends it as stdin to this workload guest:
#     aginctus-ssh-authorize reconcile agent
#
# Commands:
#   reconcile <user>  Replace Aginctus's managed key file with stdin.
#   inspect <user>    Print the managed key file without changing it.
#   remove <user>     Remove only Aginctus's managed key file.
#
# Exit codes:
#   0  success
#   1  unsafe/corrupt authorization state
#   2  invalid invocation/user
#   3  inspect requested but no managed authorization exists
#
# sshd is configured by flake.nix to consult
# /var/lib/aginctus/ssh/authorized_keys/%u in addition to the user's normal
# ~/.ssh/authorized_keys. This keeps Aginctus authorization separate from any
# administrator-managed keys.

base_dir=/var/lib/aginctus/ssh
authorized_dir="$base_dir/authorized_keys"

usage() {
  echo "usage: aginctus-ssh-authorize <reconcile|inspect|remove> <user>" >&2
  exit 2
}

validate_user() {
  [[ "$1" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || { echo "invalid login user" >&2; exit 2; }
  [[ "$1" != "root" ]] || { echo "root authorization is forbidden" >&2; exit 2; }
  id -u "$1" >/dev/null 2>&1 || { echo "login user does not exist" >&2; exit 1; }
}

# Refuse symlink traversal and foreign ownership before touching the managed
# authorization tree.
validate_existing_paths() {
  for path in "$base_dir" "$authorized_dir"; do
    if [[ -e "$path" ]]; then
      [[ -d "$path" && ! -L "$path" ]] || { echo "unsafe authorization path" >&2; exit 1; }
      [[ "$(stat -c %u "$path")" == "0" ]] || { echo "authorization path is not root-owned" >&2; exit 1; }
    fi
  done
}

# Accept only the key format emitted by aginctus-ssh-client-identity. The
# generation comment gives later rotation logic an unambiguous managed-key
# marker without relying on filenames or administrator SSH state.
validate_key_file() {
  local file=$1
  [[ -s "$file" ]] || { echo "no authorized keys supplied" >&2; exit 1; }
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^ssh-ed25519[[:space:]][A-Za-z0-9+/=]+[[:space:]]aginctus:[1-9][0-9]*$ ]] || { echo "invalid managed authorized key" >&2; exit 1; }
  done <"$file"
  ssh-keygen -lf "$file" -E sha256 >/dev/null
}

# Replace only Aginctus's dedicated authorization file. Administrator keys in
# ~/.ssh/authorized_keys are intentionally outside this helper's ownership.
reconcile() {
  local user=$1
  validate_existing_paths
  install -d -m 0755 -o root -g root "$base_dir" "$authorized_dir"

  local target="$authorized_dir/$user"
  if [[ -e "$target" ]]; then
    [[ -f "$target" && ! -L "$target" && "$(stat -c %u "$target")" == "0" ]] || { echo "unsafe authorized key target" >&2; exit 1; }
  fi

  # Read public key material from stdin into a same-filesystem temporary file,
  # validate it completely, then atomically rename it into place.
  local tmp
  tmp="$(mktemp "$authorized_dir/.$user.XXXXXX")"
  trap 'rm -f "$tmp"' EXIT
  cat >"$tmp"
  validate_key_file "$tmp"
  chmod 0644 "$tmp"
  chown root:root "$tmp"
  mv "$tmp" "$target"
  trap - EXIT
}

# Read-only path used by dry-run/reconciliation discovery.
inspect() {
  local user=$1
  validate_existing_paths
  local target="$authorized_dir/$user"
  [[ -f "$target" && ! -L "$target" ]] || exit 3
  [[ "$(stat -c %u "$target")" == "0" ]] || { echo "authorized key target is not root-owned" >&2; exit 1; }
  validate_key_file "$target"
  cat "$target"
}

# Remove only the managed file. This is intentionally idempotent and never
# edits the user's ordinary ~/.ssh/authorized_keys.
remove() {
  local user=$1
  validate_existing_paths
  local target="$authorized_dir/$user"
  [[ -e "$target" ]] || return 0
  [[ -f "$target" && ! -L "$target" && "$(stat -c %u "$target")" == "0" ]] || { echo "unsafe authorized key target" >&2; exit 1; }
  validate_key_file "$target"
  rm -- "$target"
}

[[ $# -eq 2 ]] || usage
operation=$1
user=$2
validate_user "$user"

case "$operation" in
  reconcile) reconcile "$user" ;;
  inspect) inspect "$user" ;;
  remove) remove "$user" ;;
  *) usage ;;
esac
