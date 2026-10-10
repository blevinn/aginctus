{
  self,
}:
{
  config,
  lib,
  pkgs,
  ...
}:

let
  inherit (lib)
    mkEnableOption
    mkIf
    mkOption
    types
    ;

  cfg = config.services.aginctus;
  system = pkgs.stdenv.hostPlatform.system;
  jsonFormat = pkgs.formats.json { };

  defaultHerdrImage = self.packages.${system}.herdr-client;
  defaultOpenCodeImage = self.packages.${system}.opencode-workload;

  aginctusCommand = if cfg.package == null then "aginctus" else "${cfg.package}/bin/aginctus";

  configFile = jsonFormat.generate "aginctus-config.json" cfg.settings;

  imageUpdateService =
    {
      name,
      alias,
      source,
    }:
    {
      description = "Update Aginctus Incus image ${alias}";
      wantedBy = [ "multi-user.target" ];
      after = [ "incus.service" ];
      wants = [ "incus.service" ];
      restartTriggers = [ source ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        ExecStart = "${aginctusCommand} --configuration-file ${configFile} images update ${lib.escapeShellArg alias} ${source}";
      };
    };

  knownSettingsOptions = {
    gateway = {
      id = mkOption {
        type = types.str;
        default = "local";
      };
      compose.project = mkOption {
        type = types.str;
        default = "aginctus-gateway";
      };
      images = {
        litellm = mkOption {
          type = types.str;
          default = "ghcr.io/berriai/litellm:v1.103.0";
        };
        postgres = mkOption {
          type = types.str;
          default = "docker.io/library/postgres:17-alpine";
        };
      };
    };

    infrastructure.herdr = {
      name = mkOption {
        type = types.str;
        default = "aginctus-herdr";
      };
      image.alias = mkOption {
        type = types.str;
        default = "aginctus-herdr-client";
      };
      storage.pool = mkOption {
        type = types.str;
        default = "default";
      };
    };

    incus.management.network = {
      name = mkOption {
        type = types.str;
        default = "aginctus-mgmt";
      };
      ipv4 = {
        address = mkOption {
          type = types.str;
          default = "auto";
        };
        nat = mkOption {
          type = types.bool;
          default = false;
        };
        routing = mkOption {
          type = types.bool;
          default = false;
        };
      };
      ipv6.address = mkOption {
        type = types.str;
        default = "none";
      };
    };

    workloads = mkOption {
      type = types.attrsOf (
        types.submodule {
          freeformType = jsonFormat.type;
          options = {
            instance.name = mkOption {
              type = types.str;
            };
            runtime.type = mkOption {
              type = types.str;
            };
            isolation = mkOption {
              type = types.enum [
                "container"
                "vm"
              ];
            };
            image.alias = mkOption {
              type = types.str;
            };
            storage.pool = mkOption {
              type = types.str;
              default = "default";
            };
          };
        }
      );
      default = {
        dev = {
          instance.name = "aginctus-dev";
          runtime.type = "opencode";
          isolation = "container";
          image.alias = "aginctus-opencode-workload";
          storage.pool = "default";
        };
      };
    };
  };
in
{
  options.services.aginctus = {
    enable = mkEnableOption "Aginctus host integration";

    package = mkOption {
      type = types.nullOr types.package;
      default = null;
      description = ''
        Aginctus CLI package. This remains explicit until the flake exposes a
        reproducible CLI package in addition to its guest image outputs.
      '';
    };

    settings = mkOption {
      type = types.submodule {
        freeformType = jsonFormat.type;
        options = knownSettingsOptions;
      };
      default = { };
      description = ''
        Aginctus configuration written to /etc/aginctus/config.json. Known
        settings are typed, while the freeform JSON layer preserves forward
        compatibility with newly introduced Aginctus configuration keys.
        Runtime gateway secrets are intentionally not modeled here.
      '';
    };

    images = {
      herdrClient = {
        enable = mkOption {
          type = types.bool;
          default = true;
        };
        alias = mkOption {
          type = types.str;
          default = cfg.settings.infrastructure.herdr.image.alias;
        };
        source = mkOption {
          type = types.package;
          default = defaultHerdrImage;
        };
      };

      opencodeWorkload = {
        enable = mkOption {
          type = types.bool;
          default = true;
        };
        alias = mkOption {
          type = types.str;
          default = "aginctus-opencode-workload";
        };
        source = mkOption {
          type = types.package;
          default = defaultOpenCodeImage;
        };
      };
    };

    reconcile = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Reconcile the management network, Herdr client, and selected workloads at boot.";
      };
      workloads = mkOption {
        type = types.listOf types.str;
        default = [ ];
        description = "Workload IDs to reconcile when host reconciliation is enabled.";
      };
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.package != null;
        message = "services.aginctus.package must be set while the CLI package output is not yet available from the flake";
      }
    ];

    environment.etc."aginctus/config.json".source = configFile;

    systemd.services =
      lib.optionalAttrs cfg.images.herdrClient.enable {
        aginctus-image-herdr-client = imageUpdateService {
          name = "herdr-client";
          alias = cfg.images.herdrClient.alias;
          source = cfg.images.herdrClient.source;
        };
      }
      // lib.optionalAttrs cfg.images.opencodeWorkload.enable {
        aginctus-image-opencode-workload = imageUpdateService {
          name = "opencode-workload";
          alias = cfg.images.opencodeWorkload.alias;
          source = cfg.images.opencodeWorkload.source;
        };
      }
      // lib.optionalAttrs cfg.reconcile.enable {
        aginctus-reconcile = {
          description = "Reconcile Aginctus host resources";
          wantedBy = [ "multi-user.target" ];
          after = [
            "incus.service"
            "aginctus-image-herdr-client.service"
            "aginctus-image-opencode-workload.service"
          ];
          wants = [ "incus.service" ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
          };
          script = ''
            set -eu
            ${aginctusCommand} --configuration-file ${configFile} network ensure
            ${aginctusCommand} --configuration-file ${configFile} herdr client ensure
            ${lib.concatMapStringsSep "\n" (
              id: "${aginctusCommand} --configuration-file ${configFile} workload ${lib.escapeShellArg id} ensure"
            ) cfg.reconcile.workloads}
          '';
        };
      };
  };
}
