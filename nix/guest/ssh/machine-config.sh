#!/usr/bin/env bash
set -euo pipefail

# Managed Herdr SSH machine connection. Network addresses and generations
# are validated by the Aginctus host CLI before reaching this helper.
usage() {
  echo "usage: aginctus-ssh-machine-config reconcile|inspect <alias> <ipv4> <generation>" >&2
  exit 2
}
[[ $# -eq 4 ]] || usage
action=$1
alias=$2
address=$3
generation=$4

[[ "$action" == inspect || "$action" == reconcile ]] || usage
[[ "$alias" =~ ^aginctus-[a-zA-Z0-9_-]+$ ]] || usage
[[ "$generation" =~ ^[1-9][0-9]*$ ]] || usage
[[ "$address" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage

root=/var/lib/aginctus/ssh
dir="$root/machines"
file="$dir/$alias.conf"

# Use the guest's persistent, root-owned SSH identity and verified host trust.
expected="$(cat <<EOF
Host $alias
    HostName $address
    User agent
    Port 22
    IdentityFile $root/identities/$generation/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    BatchMode yes
    StrictHostKeyChecking yes
    UpdateHostKeys no
    UserKnownHostsFile $root/trust/known_hosts
    GlobalKnownHostsFile /dev/null
    HostKeyAlias $alias
    HostKeyAlgorithms ssh-ed25519
    ConnectTimeout 5
EOF
)"

[[ -d "$root" && ! -L "$root" && "$(stat -c %u "$root")" == 0 ]] || { echo "unsafe SSH state root" >&2; exit 1; }
if [[ -e "$dir" || -L "$dir" ]]; then
  [[ -d "$dir" && ! -L "$dir" && "$(stat -c %u "$dir")" == 0 ]] || { echo "unsafe machine config directory" >&2; exit 1; }
fi
if [[ -e "$file" || -L "$file" ]]; then
  [[ -f "$file" && ! -L "$file" && "$(stat -c %u "$file")" == 0 ]] || { echo "unsafe existing machine config" >&2; exit 1; }
  [[ "$(cat "$file")" == "$expected" ]] || { echo "managed SSH config drift; explicit recovery required" >&2; exit 1; }
  exit 0
fi
[[ "$action" == reconcile ]] || exit 3
install -d -m 0700 -- "$dir"
tmp="$(mktemp "$dir/.machine.XXXXXXXX")"
trap 'rm -f "$tmp"' EXIT
printf '%s\n' "$expected" > "$tmp"
chmod 0600 -- "$tmp"
# Refuse a concurrent overwrite.
if ! ln -- "$tmp" "$file" 2>/dev/null; then
  echo "machine config created concurrently; retry inspection" >&2
  exit 1
fi
rm -f -- "$tmp"
trap - EXIT
