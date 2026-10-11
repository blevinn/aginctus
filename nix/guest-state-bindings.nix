# Guest-only NixOS module; the Incus host does not need NixOS.
# Use: import ./guest-state-bindings.nix { inherit bindings; }
{ bindings }:
{ lib, pkgs, ... }:
let
  rootFor = class:
    if class == "system" then "/var/lib/aginctus/state/system"
    else if class == "user" then "/var/lib/aginctus/state/user"
    else throw "unsupported Aginctus state class";
  validSegment = name: builtins.match "[a-zA-Z0-9][a-zA-Z0-9_-]*" name != null;
  validBinding = b:
    assert validSegment b.subdir;
    assert lib.hasPrefix "/" b.target && b.target != "/" && !(lib.hasSuffix "/" b.target);
    true;
  mountUnit = target: (lib.removePrefix "/" (lib.replaceStrings [ "/" ] [ "-" ] target)) + ".mount";
  mounts = map (b:
    assert validBinding b;
    {
      what = (rootFor b.class) + "/" + b.subdir;
      where = b.target;
      type = "none";
      options = "bind";
      wantedBy = [ "multi-user.target" ];
      requires = [ "aginctus-state-prepare.service" ];
      after = [ "aginctus-state-prepare.service" ];
    }) bindings;
  prepared = lib.concatMapStringsSep "\n" (b: ''
    ${pkgs.util-linux}/bin/mountpoint -q -- ${lib.escapeShellArg (rootFor b.class)} ||
      { echo "required Aginctus state volume absent" >&2; exit 1; }
    test ! -L ${lib.escapeShellArg ((rootFor b.class) + "/" + b.subdir)} ||
      { echo "symlink in persistent state" >&2; exit 1; }
    test ! -L ${lib.escapeShellArg b.target} ||
      { echo "symlink in bind destination" >&2; exit 1; }
    ${pkgs.coreutils}/bin/install -d -m 0700 -- ${lib.escapeShellArg ((rootFor b.class) + "/" + b.subdir)}
    ${pkgs.coreutils}/bin/install -d -m 0700 -- ${lib.escapeShellArg b.target}
    test -z "$(${pkgs.findutils}/bin/find ${lib.escapeShellArg b.target} -mindepth 1 -maxdepth 1 -print -quit)" ||
      { echo "bind destination has pre-existing contents" >&2; exit 1; }
  '') bindings;
in
{
  systemd.services.aginctus-state-prepare = {
    description = "Prepare declared Aginctus persistent bind mounts";
    wantedBy = [ "multi-user.target" ];
    before = map (b: mountUnit b.target) bindings;
    requiresMountsFor = lib.unique (map (b: rootFor b.class) bindings);
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
    script = ''
      set -eu
      ${prepared}
    '';
  };
  systemd.mounts = mounts;
}
