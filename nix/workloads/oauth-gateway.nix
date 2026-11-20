# Isolated OAuth gateway workload image; not yet enabled in live orchestration.
{ pkgs, nixpkgs, system, oauthGatewayDaemon }:
let
oauthGatewaySystem = nixpkgs.lib.nixosSystem {
  inherit system;
  modules = [
    "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
    (
      { ... }:
      {
        system.stateVersion = "26.05";
        users.groups.aginctus-oauth = { };
        users.users.aginctus-oauth = {
          isSystemUser = true;
          group = "aginctus-oauth";
          home = "/var/lib/aginctus/oauth";
        };
        environment.systemPackages = [ oauthGatewayDaemon ];
        systemd.services.aginctus-oauth-gateway = {
          description = "Aginctus isolated OAuth provider backend (not inference-ready)";
          wantedBy = [ "multi-user.target" ];
          requires = [ "var-lib-aginctus-oauth.mount" ];
          after = [ "var-lib-aginctus-oauth.mount" ];
          unitConfig.AssertPathIsMountPoint = "/var/lib/aginctus/oauth";
          serviceConfig = {
            Type = "simple";
            User = "aginctus-oauth";
            Group = "aginctus-oauth";
            WorkingDirectory = "/var/lib/aginctus/oauth";
            ExecStart = "${oauthGatewayDaemon}/bin/aginctus-oauth-gateway";
            Restart = "on-failure";
            NoNewPrivileges = true;
            PrivateTmp = true;
            ProtectSystem = "strict";
            ProtectHome = true;
            ReadWritePaths = [ "/var/lib/aginctus/oauth" ];
          };
        };
      }
    )
  ];
};
oauthGatewayRootfs = oauthGatewaySystem.config.system.build.squashfs;
oauthGatewayMetadata = oauthGatewaySystem.config.system.build.metadata;

in
pkgs.runCommand "aginctus-oauth-gateway-image" { } ''
  mkdir -p "$out"
  ln -s ${oauthGatewayRootfs} "$out/rootfs"
  ln -s ${oauthGatewayMetadata} "$out/metadata"
'';

