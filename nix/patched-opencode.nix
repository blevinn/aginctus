{ pkgs }:
# The patch is explicitly pinned to the Nixpkgs v1.15.10 source build.
# Preserve the upstream derivation's src, node_modules, hashes and build phases.
assert pkgs.opencode.version == "1.15.10";
pkgs.opencode.overrideAttrs (old: {
  patches = (old.patches or [ ]) ++ [
    ./patches/opencode-1.15.10-run-event-drain.patch
  ];
})
