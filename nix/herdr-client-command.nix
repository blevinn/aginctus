# Non-login Incus exec does not source NixOS environment.variables.
{ pkgs, herdrPackage }:
pkgs.writeShellScriptBin "herdr" ''
  for path in /var/lib/aginctus/state/system /var/lib/herdr /var/lib/aginctus/ssh; do
    ${pkgs.util-linux}/bin/mountpoint -q -- "$path" || {
      echo "required Herdr persistent mount absent" >&2
      exit 1
    }
  done
  export XDG_CONFIG_HOME=/var/lib/herdr/config
  export XDG_RUNTIME_DIR=/run/herdr
  export XDG_STATE_HOME=/var/lib/herdr
  exec ${herdrPackage}/bin/herdr "$@"
''
