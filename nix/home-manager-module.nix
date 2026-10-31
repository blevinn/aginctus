{ self }:
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

  cfg = config.programs.aginctus;
  jsonFormat = pkgs.formats.json { };
  knownSettingsOptions = import ./settings-options.nix { inherit lib jsonFormat; };
  # Render the MCP subtree in the shape the Aginctus Go schema accepts.
  mcpSettings = import ./mcp-settings.nix { inherit lib; };
  # Export the declared user CLI package, never credentials or environment.
  operatorPackageManifest = jsonFormat.generate "aginctus-user-package.json" {
    schema = 1;
    authority = "user";
    package = toString cfg.package;
  };

  # Both dotted keys and nested objects are possible in the freeform JSON
  # layer. Reject runtime credential paths before writing the Nix store.
  configuredSecretPaths = import ./runtime-secret-paths.nix { inherit lib; } cfg.settings;
in
{
  options.programs.aginctus = {
    enable = mkEnableOption "Aginctus operator CLI and nonsecret user configuration";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.aginctus;
      description = "Aginctus CLI installed in the operator's Home Manager profile.";
    };

    settings = mkOption {
      type = types.submodule {
        freeformType = jsonFormat.type;
        options = knownSettingsOptions;
      };
      default = { };
      description = ''
        Nonsecret Aginctus configuration written to the user's XDG config
        directory. Runtime gateway credentials are forbidden here; they
        must remain in protected state outside the Nix store.
      '';
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = configuredSecretPaths == [ ];
        message =
          "programs.aginctus.settings cannot contain runtime gateway secrets: "
          + lib.concatStringsSep ", " configuredSecretPaths;
      }
    ]
    ++ mcpSettings.assertions "programs.aginctus" cfg.settings.mcp;

    home.packages = [ cfg.package ];
    xdg.configFile."aginctus/operator-package.json".source = operatorPackageManifest;
    xdg.configFile."aginctus/config.json".source = jsonFormat.generate "aginctus-user-config.json" (
      mcpSettings.renderSettings cfg.settings
    );
  };
}
