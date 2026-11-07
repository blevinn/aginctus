{ lib, renderServer }:
settings:
let
  inherit (lib) mapAttrs optionalAttrs;
  providers = mapAttrs (_: provider: {
    inherit (provider) plugin;
    config = provider.config // {
      mcps = mapAttrs (_: renderServer) provider.config.mcps;
    };
  }) settings.gateway.providers;
in
settings
// {
  gateway =
    builtins.removeAttrs settings.gateway [
      "providers"
      "catalogs"
    ]
    // optionalAttrs (providers != { }) { inherit providers; }
    // optionalAttrs (settings.gateway.catalogs != { }) { catalogs = settings.gateway.catalogs; };
}
