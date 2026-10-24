{ lib, jsonFormat }:
let
  inherit (lib) mkOption types;
in
{
  firewall.zone = mkOption {
    type = types.str;
    default = "aginctus-dns";
    description = "Dedicated firewalld zone to manage for the selected Aginctus profile (max 17 characters, aginctus- prefix).";
  };

  firewall.egress.allowTailscaleRouting = mkOption {
    type = types.bool;
    default = true;
    description = "Accept existing standard Tailscale IPv4 policy-routing rules during egress verification. Set false to require kernel-only rules. Never installs or modifies routing rules.";
  };

  firewall.egress.uplinkZone = mkOption {
    type = types.str;
    default = "";
    description = "Runtime HTTPS uplink zone, verified against the unique IPv4 default route. Empty fails closed.";
  };

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
}
