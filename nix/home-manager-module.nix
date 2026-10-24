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

  # Both dotted keys and nested objects are possible in the freeform JSON
  # layer. Reject runtime credential paths before writing the Nix store.
  runtimeSecretKeys = [
    "gateway.postgres.password"
    "gateway.master.key"
    "gateway.salt.key"
  ];
  configuredSecretPaths =
    let
      visit =
        prefix: value:
        if builtins.isAttrs value then
          lib.concatLists (
            lib.mapAttrsToList (
              name: nested:
              let
                path = prefix ++ [ name ];
                dotted = lib.concatStringsSep "." path;
              in
              lib.optional (lib.elem (lib.toLower dotted) runtimeSecretKeys) dotted ++ visit path nested
            ) value
          )
        else
          [ ];
    in
    visit [ ] cfg.settings;
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
    ];

    home.packages = [ cfg.package ];
    xdg.configFile."aginctus/config.json".source =
      jsonFormat.generate "aginctus-user-config.json" cfg.settings;
  };
}
