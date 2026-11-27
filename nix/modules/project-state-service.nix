{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.aginctus-project-state;
  package = self.packages.${pkgs.stdenv.hostPlatform.system}.aginctus;
in
{
  options.services.aginctus-project-state = {
    enable = lib.mkEnableOption "the manually started, administrator-controlled Aginctus project-state guest role";
    uid = lib.mkOption {
      type = lib.types.ints.positive;
      description = "Dedicated guest service UID/GID; must match bootstrap configuration and retained mount ownership.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.groups.aginctusproject.gid = cfg.uid;
    users.users.aginctusproject = {
      inherit (cfg) uid;
      isSystemUser = true;
      group = "aginctusproject";
    };

    # No wantedBy: the administrator owns attachment preflight, config and
    # startup. Restart revalidates the mounted store using the same service UID.
    systemd.services.aginctus-project-state = {
      description = "Aginctus project state (administrator-controlled)";
      restartIfChanged = false;
      serviceConfig = {
        Type = "exec";
        User = "aginctusproject";
        Group = "aginctusproject";
        UMask = "0077";
        NoNewPrivileges = true;
        CapabilityBoundingSet = "";
        Environment = "AGINCTUS_PROJECT_STATE_READ_SERVICE_CONFIG=/run/aginctus/state.json";
        ExecStartPre = "${package}/bin/aginctus serve project-state bootstrap";
        ExecStart = "${package}/bin/aginctus serve project-state";
      };
    };
  };
}
