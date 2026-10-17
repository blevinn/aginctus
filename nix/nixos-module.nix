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
  firewalldBackend = config.services.firewalld.enable;
  system = pkgs.stdenv.hostPlatform.system;
  jsonFormat = pkgs.formats.json { };

  defaultHerdrImage = self.packages.${system}.herdr-client;
  defaultOpenCodeImage = self.packages.${system}.opencode-workload;

  aginctusCommand = "${cfg.package}/bin/aginctus";

  # The Go config loader treats these as runtime-only secrets, but the
  # freeform options layer would otherwise allow them into the world-readable
  # Nix store.
  forbiddenGatewaySecrets = [
    [
      "gateway"
      "postgres"
      "password"
    ]
    [
      "gateway"
      "master"
      "key"
    ]
    [
      "gateway"
      "salt"
      "key"
    ]
  ];

  configuredSecretPaths = builtins.filter (
    path: lib.hasAttrByPath path cfg.settings
  ) forbiddenGatewaySecrets;

  validImageAlias =
    alias:
    builtins.stringLength alias <= 100 && builtins.match "[A-Za-z0-9][A-Za-z0-9_.-]*" alias != null;

  enabledImageAliases =
    lib.optional cfg.images.herdrClient.enable cfg.images.herdrClient.alias
    ++ lib.optional cfg.images.opencodeWorkload.enable cfg.images.opencodeWorkload.alias;

  unknownWorkloads = builtins.filter (
    id: !(builtins.hasAttr id cfg.settings.workloads)
  ) cfg.reconcile.workloads;

  configFile = jsonFormat.generate "aginctus-config.json" cfg.settings;

  imageUpdateService =
    {
      alias,
      source,
    }:
    {
      description = "Update Aginctus Incus image ${alias}";
      wantedBy = [ "multi-user.target" ];
      after = [ "incus.service" ];
      requires = [ "incus.service" ];
      path = [ pkgs.incus ];
      restartTriggers = [
        source
        cfg.package
      ];
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
      type = types.package;
      default = self.packages.${system}.aginctus;
      description = "Aginctus CLI package used by generated systemd units.";
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

    firewall.enable = mkOption {
      type = types.bool;
      default = true;
      description = "Reconcile ownership-scoped Incus DNS/DHCP exceptions with the NixOS iptables-nft or firewalld backend.";
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
        assertion =
          !cfg.firewall.enable
          || (
            config.networking.firewall.enable
            && (if firewalldBackend then config.networking.nftables.enable else !config.networking.nftables.enable)
          );
        message = "services.aginctus.firewall requires enabled host filtering with nftables for firewalld or iptables-nft for the non-firewalld NixOS backend.";
      }
      {
        assertion = configuredSecretPaths == [ ];
        message = "services.aginctus.settings cannot contain runtime gateway secrets: ${
          lib.concatStringsSep ", " (map (path: lib.concatStringsSep "." path) configuredSecretPaths)
        }. Provide these through the supported runtime credential mechanism instead.";
      }
      {
        assertion = builtins.all validImageAlias enabledImageAliases;
        message = "services.aginctus.images aliases must follow the Incus image alias format (1-100 ASCII letters, digits, periods, underscores, or hyphens; starting with a letter or digit).";
      }
      {
        assertion = builtins.length (lib.unique enabledImageAliases) == builtins.length enabledImageAliases;
        message = "services.aginctus.images enabled image imports must have distinct aliases.";
      }
      {
        assertion = !cfg.reconcile.enable || unknownWorkloads == [ ];
        message = "services.aginctus.reconcile.workloads contains unknown workload IDs: ${lib.concatStringsSep ", " unknownWorkloads}";
      }
    ];

    # ExtraCommands executes before the NixOS iptables firewall's final
    # reject, unlike an independent nftables accept-only base chain.
    networking.firewall.extraCommands = lib.mkIf (cfg.firewall.enable && !firewalldBackend) ''
      iptables -N AGINCTUS_INPUT 2>/dev/null || iptables -F AGINCTUS_INPUT
      iptables -C nixos-fw -j AGINCTUS_INPUT 2>/dev/null || iptables -I nixos-fw 1 -j AGINCTUS_INPUT
      # On Incus errors the empty chain must remain fail-closed.
      ${aginctusCommand} --configuration-file ${configFile} network firewall reconcile || true
    '';
    networking.firewall.extraPackages = lib.mkIf (cfg.firewall.enable && !firewalldBackend) [
      pkgs.nftables
    ];
    networking.firewall.extraStopCommands = lib.mkIf (cfg.firewall.enable && !firewalldBackend) ''
      iptables -D nixos-fw -j AGINCTUS_INPUT 2>/dev/null || true
      iptables -F AGINCTUS_INPUT 2>/dev/null || true
      iptables -X AGINCTUS_INPUT 2>/dev/null || true
    '';

    # Firewalld owns its nftables table (flags owner,persist), so Aginctus
    # must use firewall-cmd/D-Bus instead of editing that table directly.
    # Only this empty, DROP-by-default zone is permanent. Verified bridge
    # bindings and narrow exceptions are installed at runtime by the service;
    # firewalld reloads remove them until the next reconciliation.
    environment.etc."firewalld/zones/aginctus-dns.xml" = lib.mkIf (cfg.firewall.enable && firewalldBackend) {
      text = ''
        <?xml version="1.0" encoding="utf-8"?>
        <zone target="DROP">
          <short>Aginctus DNS/DHCP</short>
          <description>Runtime-only permissions for one Incus-owned management bridge.</description>
        </zone>
      '';
    };

    environment.etc."aginctus/config.json".source = configFile;

    systemd.timers.aginctus-firewall-reconcile = lib.mkIf cfg.firewall.enable {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "15s";
        OnUnitActiveSec = "30s";
        Unit = "aginctus-firewall-reconcile.service";
      };
    };

    systemd.services =
      lib.optionalAttrs cfg.firewall.enable {
        aginctus-firewall-reconcile = {
          description = "Reconcile Aginctus-owned Incus guest-to-host DNS/DHCP policy";
          wantedBy = [ "multi-user.target" ];
          after = [
            "incus.service"
            "firewall.service"
          ]
          ++ lib.optional firewalldBackend "firewalld.service";
          wants = [
            "incus.service"
            "firewall.service"
          ]
          ++ lib.optional firewalldBackend "firewalld.service";
          path = [
            pkgs.iptables
            pkgs.nftables
            pkgs.incus
          ] ++ lib.optional firewalldBackend pkgs.firewalld;
          serviceConfig = {
            Type = "oneshot";
            User = "root";
            # ProtectHome hides /root; do not probe a root-user override file.
            # The managed configuration is passed explicitly via --configuration-file.
            Environment = [ "XDG_CONFIG_HOME=/var/empty" ];
            NoNewPrivileges = true;
            PrivateTmp = true;
            ProtectHome = true;
            CapabilityBoundingSet = [ "CAP_NET_ADMIN" ];
            AmbientCapabilities = [ "CAP_NET_ADMIN" ];
            # Reconciliation must not misidentify an uninitialized firewalld
            # nftables table as the iptables-nft backend during daemon restart.
            # Wait a bounded time for both D-Bus and the effective INPUT hook.
            ExecStartPre = lib.optional firewalldBackend (
              "${pkgs.writeShellScript "aginctus-firewalld-ready" ''
                for attempt in {1..50}; do
                  if ${pkgs.nftables}/bin/nft list chain inet firewalld filter_INPUT >/dev/null 2>&1 && ${pkgs.firewalld}/bin/firewall-cmd --state >/dev/null 2>&1; then
                    exit 0
                  fi
                  ${pkgs.coreutils}/bin/sleep 0.2
                done
                echo 'firewalld INPUT chain is not ready for reconciliation' >&2
                exit 1
              ''}"
            );
            ExecStart = "${aginctusCommand} --configuration-file ${configFile} --config firewall.backend=${if firewalldBackend then "firewalld" else "iptables-nft"} network firewall reconcile";
          };
        };
      }
      // lib.optionalAttrs cfg.images.herdrClient.enable {
        aginctus-image-herdr-client = imageUpdateService {
          alias = cfg.images.herdrClient.alias;
          source = cfg.images.herdrClient.source;
        };
      }
      // lib.optionalAttrs cfg.images.opencodeWorkload.enable {
        aginctus-image-opencode-workload = imageUpdateService {
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
          ]
          ++ lib.optional cfg.images.herdrClient.enable "aginctus-image-herdr-client.service"
          ++ lib.optional cfg.images.opencodeWorkload.enable "aginctus-image-opencode-workload.service";
          requires = [
            "incus.service"
          ]
          ++ lib.optional cfg.images.herdrClient.enable "aginctus-image-herdr-client.service"
          ++ lib.optional cfg.images.opencodeWorkload.enable "aginctus-image-opencode-workload.service";
          path = [ pkgs.incus ];
          restartTriggers = [
            configFile
            cfg.package
          ]
          ++ lib.optional cfg.images.herdrClient.enable cfg.images.herdrClient.source
          ++ lib.optional cfg.images.opencodeWorkload.enable cfg.images.opencodeWorkload.source;
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
