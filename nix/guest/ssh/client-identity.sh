#!/usr/bin/env bash
set -euo pipefail

# Guest-side helper installed only in the managed Herdr client image.
#
# This script is NOT run by a human over SSH. Aginctus invokes it through
# authenticated Incus guest exec after the Herdr client instance is running.
#
# Responsibilities:
#   * own the Herdr client's SSH private key lifecycle;
#   * keep private key material inside this guest;
#   * expose only generation, fingerprint, and public key to the caller.
#
# Commands:
#   ensure   Create generation 1 if no identity exists, then print it.
#   inspect  Print the existing identity without mutating state.
#
# stdout is a small tab-delimited protocol consumed by Aginctus:
#   generation<TAB>1
#   fingerprint<TAB>SHA256:...
#   public_key<TAB>ssh-ed25519 ... aginctus:1
#
# Exit codes:
#   0  success
#   1  unsafe/corrupt identity state
#   2  invalid invocation
#   3  inspect requested but no identity exists
#
# The companion workload helper is aginctus-ssh-authorize. Aginctus takes only
# the public_key line from this helper and sends that public key to the workload
# helper over a separate Incus exec call. The private key never leaves this guest.

state_dir=/var/lib/aginctus/ssh
identities_dir="$state_dir/identities"
manifest="$state_dir/manifest"
lock_file="$state_dir/.lock"

usage() {
  echo "usage: aginctus-ssh-client-identity <ensure|inspect>" >&2
  exit 2
}

read_manifest_value() {
  local key=$1
  local file=$2
  sed -n "s/^$key=//p" "$file"
}

# Refuse to operate through symlinks or non-root-owned state directories. These
# paths contain the long-lived private key and are therefore treated as trusted
# guest-local state, not as scratch space.
validate_state_paths() {
  for path in "$state_dir" "$identities_dir"; do
    if [[ -e "$path" ]]; then
      [[ -d "$path" && ! -L "$path" ]] || { echo "unsafe ssh state path" >&2; exit 1; }
      [[ "$(stat -c %u "$path")" == "0" ]] || { echo "ssh state path is not root-owned" >&2; exit 1; }
    fi
  done
}

# Validate the selected generation and emit only public metadata. Deriving the
# public key from the private key on every read catches partial/corrupt state
# before Aginctus distributes anything to a workload.
emit_identity() {
  local generation=$1
  local key_dir="$identities_dir/$generation"
  local private_key="$key_dir/id_ed25519"
  local public_key="$key_dir/id_ed25519.pub"

  [[ -f "$private_key" && ! -L "$private_key" ]] || { echo "missing client private key" >&2; exit 1; }
  [[ -f "$public_key" && ! -L "$public_key" ]] || { echo "missing client public key" >&2; exit 1; }
  [[ "$(stat -c %u "$private_key")" == "0" && "$(stat -c %a "$private_key")" == "600" ]] || { echo "unsafe client private key ownership or mode" >&2; exit 1; }
  [[ "$(stat -c %u "$public_key")" == "0" ]] || { echo "client public key is not root-owned" >&2; exit 1; }

  local derived expected fingerprint
  # Recent OpenSSH versions preserve the private key comment in `ssh-keygen -y`
  # output. Compare only the cryptographic key type + base64 payload so comments
  # cannot create a false mismatch.
  derived="$(ssh-keygen -y -f "$private_key" | awk '{print $1 " " $2}')"
  expected="$(awk '{print $1 " " $2}' "$public_key")"
  [[ "$derived" == "$expected" ]] || { echo "client keypair mismatch" >&2; exit 1; }
  fingerprint="$(ssh-keygen -lf "$public_key" -E sha256 | awk '{print $2}')"

  printf 'generation\t%s\n' "$generation"
  printf 'fingerprint\t%s\n' "$fingerprint"
  printf 'public_key\t%s\n' "$(cat "$public_key")"
}

# Read-only path used by dry-run/reconciliation discovery. Exit 3 is deliberate:
# it lets the host distinguish "not created yet" from corrupt state.
inspect_identity() {
  validate_state_paths
  [[ -f "$manifest" && ! -L "$manifest" ]] || exit 3
  [[ "$(stat -c %u "$manifest")" == "0" ]] || { echo "ssh identity manifest is not root-owned" >&2; exit 1; }

  local generation
  generation="$(read_manifest_value active_generation "$manifest")"
  [[ "$generation" =~ ^[1-9][0-9]*$ ]] || { echo "invalid active generation" >&2; exit 1; }
  emit_identity "$generation"
}

# Mutating path. Creation is serialized because concurrent host reconciles must
# never create competing identities. The generation directory and manifest are
# both published atomically.
ensure_identity() {
  umask 077
  validate_state_paths
  install -d -m 0700 -o root -g root "$state_dir" "$identities_dir"

  exec 9>"$lock_file"
  flock -x 9

  if [[ -e "$manifest" ]]; then
    inspect_identity
    return
  fi

  local generation=1
  local final_dir="$identities_dir/$generation"
  [[ ! -e "$final_dir" ]] || { echo "identity directory exists without manifest" >&2; exit 1; }

  local tmp_dir
  tmp_dir="$(mktemp -d "$identities_dir/.generation-$generation.XXXXXX")"
  trap 'rm -rf "$tmp_dir"' EXIT

  # The comment is part of the managed workload authorization contract. The
  # workload helper accepts only Ed25519 keys with an aginctus:<generation>
  # comment, so unrelated administrator keys cannot be confused with this key.
  ssh-keygen -q -t ed25519 -N "" -C "aginctus:$generation" -f "$tmp_dir/id_ed25519"
  chmod 0600 "$tmp_dir/id_ed25519"
  chmod 0644 "$tmp_dir/id_ed25519.pub"

  local derived expected
  derived="$(ssh-keygen -y -f "$tmp_dir/id_ed25519" | awk '{print $1 " " $2}')"
  expected="$(awk '{print $1 " " $2}' "$tmp_dir/id_ed25519.pub")"
  [[ "$derived" == "$expected" ]] || { echo "generated client keypair mismatch" >&2; exit 1; }

  mv "$tmp_dir" "$final_dir"
  trap - EXIT

  local tmp_manifest
  tmp_manifest="$(mktemp "$state_dir/.manifest.XXXXXX")"
  printf 'active_generation=%s\n' "$generation" >"$tmp_manifest"
  chmod 0600 "$tmp_manifest"
  mv "$tmp_manifest" "$manifest"

  emit_identity "$generation"
}

[[ $# -eq 1 ]] || usage
case "$1" in
  ensure) ensure_identity ;;
  inspect) inspect_identity ;;
  *) usage ;;
esac
