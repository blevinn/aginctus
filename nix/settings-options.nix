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
    litellm.selectiveGoForwarding = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Install the callback-only bundle for the fixed OpenCode Go Responses
        alias. Requires an immutable ghcr.io/berriai/litellm SHA-256 image
        reference and no stock client-header forwarding. The callback rejects
        any runtime other than LiteLLM 1.103.0 at import.
      '';
    };
    litellm.forwardClientHeaders = mkOption {
      type = types.either types.bool (types.listOf types.str);
      default = false;
      description = ''
        Forward eligible client headers globally when true, or only for the
        model groups named in a string list. False and an empty list disable
        forwarding. Lists select model groups, not header names, and cannot
        exclude models from global forwarding. This may expose additional
        x-* request headers to upstream providers.
      '';
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

  # Declarative v0.2 project target. Existing v0.1 lifecycle operations
  # are not redirected until explicit project threading and migration land.
  incus.project = mkOption {
    type = types.addCheck types.str (
      name:
      name != "default"
      && builtins.stringLength name <= 100
      && builtins.match "[A-Za-z0-9][A-Za-z0-9_.-]*" name != null
    );
    default = "aginctus";
    description = ''
      Intended dedicated Incus project for the single Aginctus deployment.
      The default Incus project is never a valid v0.2 lifecycle target.
      This option is only declarative until project-scoped callers and the
      controlled v0.1 migration are implemented.
    '';
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
