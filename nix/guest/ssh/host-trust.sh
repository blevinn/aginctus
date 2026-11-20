#!/usr/bin/env bash
# Managed SSH host trust for the Herdr client.
# Usage: aginctus-ssh-host-trust inspect <alias>
#        aginctus-ssh-host-trust reconcile <alias>   (stdin: ssh-ed25519 public host key)
# Never learns a key from the network. Caller obtains it via authenticated Incus.
set -euo pipefail

root=/var/lib/aginctus/ssh
trust="$root/trust"
known="$trust/known_hosts"

die() {
  printf 'host trust: %s\n' "$*" >&2
  exit 1
}
if [[ $# != 2 ]]; then
  printf 'Usage: aginctus-ssh-host-trust inspect|reconcile <alias>\n' >&2
  exit 2
fi
operation="$1"
alias="$2"
[[ "$alias" =~ ^aginctus-[a-zA-Z0-9_-]+$ ]] || die "invalid managed alias"

check_existing() {
  if [[ -L "$trust" || -L "$known" ]]; then die "symlink in managed trust path"; fi
  if [[ -e "$trust" && ! -d "$trust" ]]; then die "invalid trust directory"; fi
  if [[ -e "$known" && ! -f "$known" ]]; then die "invalid known_hosts file"; fi
  if [[ -e "$root" ]]; then
    [[ -d "$root" ]] || die "invalid ssh state root"
    [[ "$(stat -c '%u' "$root")" == 0 ]] || die "ssh state root not root-owned"
  fi
  if [[ -e "$trust" ]]; then
    [[ "$(stat -c '%u' "$trust")" == 0 ]] || die "trust directory not root-owned"
    [[ "$(stat -c '%a' "$trust")" == 700 ]] || die "unsafe trust directory permissions"
  fi
  if [[ -e "$known" ]]; then
    [[ "$(stat -c '%u' "$known")" == 0 ]] || die "known_hosts not root-owned"
    [[ "$(stat -c '%a' "$known")" == 600 ]] || die "unsafe known_hosts permissions"
  fi
}
check_existing

existing=""
if [[ -f "$known" ]]; then
  while IFS= read -r line || [[ -n "$line" ]]; do
    read -r -a words <<< "$line"
    if [[ "${words[0]:-}" == "$alias" ]]; then
      [[ ${#words[@]} == 3 && "${words[1]}" == ssh-ed25519 ]] || die "malformed existing pinned key"
      [[ -z "$existing" ]] || die "duplicate existing pinned alias"
      existing="$line"
    fi
  done < "$known"
fi

if [[ "$operation" == inspect ]]; then
  if [[ -z "$existing" ]]; then exit 3; fi
  printf '%s\n' "$existing"
  exit 0
fi
if [[ "$operation" != reconcile ]]; then
  printf 'unknown operation\n' >&2
  exit 2
fi

IFS= read -r incoming || die "missing host public key"
read -r -a key_fields <<< "$incoming"
[[ ${#key_fields[@]} -ge 2 && "${key_fields[0]}" == ssh-ed25519 ]] || die "expected Ed25519 host key"
[[ "${key_fields[1]}" =~ ^[A-Za-z0-9+/]+={0,2}$ ]] || die "invalid host key encoding"
new="$alias ${key_fields[0]} ${key_fields[1]}"
if [[ -n "$existing" ]]; then
  [[ "$existing" == "$new" ]] || die "existing pinned host key differs; explicit recovery required"
  printf '%s\n' "$existing"
  exit 0
fi

# Validate the actual public-key structure with OpenSSH before writing trust.
probe="$(mktemp)"
trap 'rm -f "$probe"' EXIT
printf '%s\n' "${key_fields[0]} ${key_fields[1]}" > "$probe"
ssh-keygen -lf "$probe" -E sha256 >/dev/null || die "invalid Ed25519 host key"

mkdir -p -- "$root" "$trust"
chmod 0700 "$root" "$trust"
check_existing
tmp="$(mktemp "$trust/.known_hosts.XXXXXXXX")"
trap 'rm -f "$probe" "$tmp"' EXIT
chmod 0600 "$tmp"
if [[ -f "$known" ]]; then cat "$known" > "$tmp"; fi
printf '%s\n' "$new" >> "$tmp"
mv -f -- "$tmp" "$known"
printf '%s\n' "$new"
