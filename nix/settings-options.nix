{ lib, jsonFormat }:
let
  inherit (lib) mkOption types;

  # Well-known credential formats. A credential reference that starts with
  # one of them is refused so an operator cannot paste upstream credential
  # material into declarative configuration.
  credentialPrefixes = [
    "sk-"
    "sk_"
    "pk-"
    "pk_"
    "rk-"
    "rk_"
    "ghp_"
    "gho_"
    "ghu_"
    "ghs_"
    "ghr_"
    "github_pat_"
    "xoxb-"
    "xoxp-"
    "xoxa-"
    "xoxr-"
    "akia"
    "asia"
    "aiza"
    "ya29."
    "eyj"
  ];

  # An opaque lowercase reference to protected state. It is never credential
  # material and it is never resolved during evaluation.
  credentialRefType = types.nullOr (
    types.addCheck types.str (
      value:
      builtins.match "[a-z0-9][a-z0-9._-]{0,63}" value != null
      && !(builtins.any (prefix: builtins.match ("^" + prefix) value != null) credentialPrefixes)
    )
  );

  toolNameType = types.addCheck types.str (
    value: builtins.match "[A-Za-z0-9][A-Za-z0-9_.-]{0,127}" value != null
  );
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
      dhcp.gateway = mkOption {
        type = types.str;
        default = "none";
        description = "Incus management DHCP gateway announcement. 'none' retains DHCP addressing and DNS without advertising a default route.";
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

  # Normalized MCP declarations. The subtree is strict: an unknown field is
  # an evaluation error, so a credential-bearing key can never be written
  # into the generated configuration. Nothing here activates a service.
  mcp = {
    capabilities = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            protocol = mkOption {
              type = types.enum [
                "responses"
                "chat-completions"
              ];
              default = "responses";
              description = "Model-facing protocol the capability profile targets.";
            };
            requires = mkOption {
              type = types.listOf (
                types.enum [
                  "mcp-tools"
                  "provider-hosted-tools"
                ]
              );
              default = [ ];
              description = ''
                Required capability tokens. "mcp-tools" declares a need for
                centrally enforced MCP tool calls; "provider-hosted-tools"
                declares tools the model provider executes, which never
                reach the Aginctus gateway.
              '';
            };
          };
        }
      );
      default = { };
      description = ''
        Model and protocol capability profiles referenced by MCP grants.
        A profile grants nothing by itself.
      '';
    };

    gateway = mkOption {
      type = types.submodule {
        options = {
          enabled = mkOption {
            type = types.bool;
            default = false;
            description = ''
              Declares that the central gateway MCP endpoint is intended to
              be exposed and to route the declared remote servers. This
              option states an intent only; no endpoint, listener or route
              exists yet.
            '';
          };
          requireAuthenticatedWorkload = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Declares that the gateway must bind requests to an
              authenticated workload identity. It cannot be disabled.
            '';
          };
          upstreamTimeoutSeconds = mkOption {
            type = types.addCheck types.int (value: value >= 1 && value <= 300);
            default = 30;
            description = "Bounds a single upstream MCP call.";
          };
        };
      };
      default = { };
      description = "Declared intent for the central gateway MCP endpoint.";
    };

    grants = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            capability = mkOption {
              type = types.nullOr types.str;
              default = null;
              description = ''
                Capability profile the workload requires. It is required
                when the workload holds MCP server grants.
              '';
            };
            servers = mkOption {
              type = types.attrsOf (types.listOf toolNameType);
              default = { };
              description = ''
                Explicit per-server tool grants. Each key names a declared
                server and each list names the tools the workload may call.
                An empty list grants no tool.
              '';
            };
          };
        }
      );
      default = { };
      description = ''
        Explicit per-workload MCP grants. A workload without a grant has no
        MCP access.
      '';
    };

    servers = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            transport = mkOption {
              type = types.enum [
                "streamable-http"
                "stdio"
              ];
              description = ''
                "streamable-http" is a remote server routed by the central
                gateway. "stdio" is a local process started inside the
                authorized workload; the gateway never executes it.
              '';
            };
            url = mkOption {
              type = types.nullOr types.str;
              default = null;
              description = "Streamable HTTP endpoint. Required for the streamable-http transport.";
            };
            command = mkOption {
              type = types.nullOr types.str;
              default = null;
              description = "Absolute executable path. Required for the stdio transport.";
            };
            args = mkOption {
              type = types.listOf types.str;
              default = [ ];
              description = "Arguments of the workload-local stdio process.";
            };
            credentialRef = mkOption {
              type = credentialRefType;
              default = null;
              description = ''
                Opaque reference to protected state that holds the upstream
                credential. It is never credential material and it is never
                resolved during evaluation.
              '';
            };
            allowedTools = mkOption {
              type = types.nullOr (types.listOf toolNameType);
              default = null;
              description = ''
                Server-level tool surface. Null leaves the server
                unrestricted at the server level, which still requires the
                workload to name every tool it may call.
              '';
            };
          };
        }
      );
      default = { };
      description = "Declared MCP servers. A server without a grant is unreachable.";
    };
  };
}
