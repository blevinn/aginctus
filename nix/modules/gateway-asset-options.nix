{ lib, serverType }:
let
  inherit (lib) mkOption types;
  nameType = types.addCheck types.str (
    name: builtins.match "[A-Za-z0-9][A-Za-z0-9_.-]{0,127}" name != null
  );
  referenceType = types.submodule {
    options = {
      provider = mkOption {
        type = types.str;
        default = "";
      };
      name = mkOption { type = types.str; };
      local = mkOption {
        type = types.bool;
        default = false;
      };
    };
  };
in
{
  providers = mkOption {
    default = { };
    description = "Named nonsecret compiled-plugin instances. Credentials remain in protected runtime state.";
    type = types.attrsOf (
      types.submodule {
        options = {
          plugin = mkOption { type = nameType; };
          config = mkOption {
            default = { };
            type = types.submodule {
              options = {
                state_dir = mkOption {
                  type = types.str;
                  default = "";
                  description = "Absolute normalized private state path for plugins that support it; never credential contents.";
                };
                models = mkOption {
                  default = { };
                  type = types.attrsOf (
                    types.submodule {
                      options = {
                        source = mkOption { type = types.str; };
                        display_name = mkOption {
                          type = types.str;
                          default = "";
                        };
                      };
                    }
                  );
                };
                mcps = mkOption {
                  type = types.attrsOf serverType;
                  default = { };
                  description = "Provider-offered MCP endpoints using the same server schema as settings.mcp.servers; grants are separate.";
                };
                routes = mkOption {
                  default = { };
                  description = "Managed SIWC routes resolved through catalog aliases; credentials are enrolled by provider login.";
                  type = types.attrsOf (
                    types.submodule {
                      options = {
                        catalog = mkOption { type = nameType; };
                        workload_key_file = mkOption { type = types.str; };
                      };
                    }
                  );
                };
              };
            };
          };
        };
      }
    );
  };
  catalogs = mkOption {
    default = { };
    description = "Explicit model/MCP selections and inherited alias mappings; declarations do not grant access or publish routes.";
    type = types.attrsOf (
      types.submodule {
        options = {
          models = mkOption {
            type = types.attrsOf referenceType;
            default = { };
          };
          mcps = mkOption {
            type = types.attrsOf referenceType;
            default = { };
          };
          inherits = mkOption {
            default = [ ];
            type = types.listOf (
              types.submodule {
                options = {
                  catalog = mkOption { type = nameType; };
                  models = mkOption {
                    type = types.attrsOf types.str;
                    default = { };
                  };
                  mcps = mkOption {
                    type = types.attrsOf types.str;
                    default = { };
                  };
                };
              }
            );
          };
        };
      }
    );
  };
}
