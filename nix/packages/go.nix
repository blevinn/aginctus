{ pkgs, releaseVersion, self, sourceRoot }:
let
  # One locked Go module graph shared by all independently built commands.
  vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
in
{
aginctusCli = pkgs.buildGoModule {
  pname = "aginctus";
  version = "${releaseVersion}+${self.shortRev or "source"}";
  src = sourceRoot;
  # Dirty trees and non-Git sources get content identity, not a false commit claim.
  ldflags = [
    "-X=github.com/blevinn/aginctus/internal/cli.Revision=${self.rev or "source-${self.narHash}"}"
  ];
  # Recalculate after Go module graph changes. A stale hash can reuse
  # an incompatible vendor tree even when go.mod is newer.
  inherit vendorHash;
  subPackages = [ "./cmd/aginctus" ];
  nativeBuildInputs = [ pkgs.makeWrapper ];
  postInstall = ''
    wrapProgram "$out/bin/aginctus" --prefix PATH : "${pkgs.lib.makeBinPath [ pkgs.incus ]}"
  '';
};
oauthGatewayDaemon = pkgs.buildGoModule {
  pname = "aginctus-oauth-gateway";
  version = releaseVersion;
  src = sourceRoot;
  inherit vendorHash;
  subPackages = [ "./cmd/aginctus-oauth-gateway" ];
};
testenv-cli = pkgs.buildGoModule {
  pname = "aginctus-testenv";
  version = releaseVersion;
  src = sourceRoot;
  inherit vendorHash;
  subPackages = [ "./cmd/aginctus-testenv" ];
  ldflags = [ "-X=main.revision=${self.rev or "source-${self.narHash}"}" ];
};
# Standalone opt-in accepted-state tools. Deliberately NOT installed
# by NixOS/Home Manager modules and never exposed as an MCP command.
# The service remains read-only; the local tool can promote only
# explicit pinned workload candidates in its own private same-UID store.
state-service = pkgs.buildGoModule {
  pname = "aginctus-state";
  version = releaseVersion;
  src = sourceRoot;
  inherit vendorHash;
  subPackages = [ "./cmd/aginctus-state" ];
};
state-local = pkgs.buildGoModule {
  pname = "aginctus-state-local";
  version = releaseVersion;
  src = sourceRoot;
  inherit vendorHash;
  subPackages = [ "./cmd/aginctus-state-local" ];
};
}
