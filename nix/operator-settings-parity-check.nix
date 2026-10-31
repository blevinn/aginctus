{
  self,
  nixpkgs,
  system,
}:
let
  lib = nixpkgs.lib;
  pkgs = import nixpkgs { inherit system; };
  stub = pkgs.writeShellScriptBin "aginctus" "exit 0";
  cases = builtins.fromJSON (builtins.readFile ../internal/config/testdata/operator-settings.json);
  nixos =
    settings:
    (lib.nixosSystem {
      inherit system;
      modules = [
        self.nixosModules.default
        {
          system.stateVersion = "26.05";
          services.aginctus = {
            enable = true;
            package = stub;
            images.herdrClient.enable = false;
            images.opencodeWorkload.enable = false;
            reconcile.enable = false;
            inherit settings;
          };
        }
      ];
    }).config;
  home =
    settings:
    lib.evalModules {
      specialArgs = { inherit pkgs; };
      modules = [
        self.homeManagerModules.default
        ({ lib, ... }: {
          options = {
            home.packages = lib.mkOption {
              type = lib.types.listOf lib.types.package;
              default = [ ];
            };
            xdg.configFile = lib.mkOption {
              type = lib.types.attrsOf lib.types.anything;
              default = { };
            };
            assertions = lib.mkOption {
              type = lib.types.listOf lib.types.anything;
              default = [ ];
            };
          };
        })
        {
          programs.aginctus = {
            enable = true;
            package = stub;
            inherit settings;
          };
        }
      ];
    };
  rejects =
    settings: cfg:
    let
      result = builtins.tryEval (
        builtins.deepSeq settings (
          lib.all (
            a:
            a.assertion || !(lib.hasInfix "aginctus.settings cannot contain runtime gateway secrets:" a.message)
          ) cfg.assertions
        )
      );
    in
    !result.success || !result.value;
  negativeChecks = lib.mapAttrsToList (
    _: settings:
    assert rejects (nixos settings).services.aginctus.settings (nixos settings);
    assert rejects (home settings).config.programs.aginctus.settings (home settings).config;
    true
  ) cases.invalid;
  fixtures = lib.mapAttrs (
    _: settings:
    assert !rejects (nixos settings).services.aginctus.settings (nixos settings);
    assert !rejects (home settings).config.programs.aginctus.settings (home settings).config;
    {
      nixos = (nixos settings).environment.etc."aginctus/config.json".source;
      home = (home settings).config.xdg.configFile."aginctus/config.json".source;
    }
  ) cases.valid;
  manifest = pkgs.writeText "operator-settings-manifest.json" (builtins.toJSON fixtures);
in
assert lib.all (x: x) negativeChecks;
assert !(lib.hasAttrByPath [ "services" "aginctus" ] (home { }).options);
self.packages.${system}.aginctus.overrideAttrs (old: {
  pname = "aginctus-operator-settings-parity";
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    export AGINCTUS_OPERATOR_SETTINGS_MANIFEST=${manifest}
    export AGINCTUS_NIXOS_MODULE_DEFAULTS=${fixtures.defaults.nixos}
    unset AGINCTUS_INCUS_INTEGRATION AGINCTUS_INCUS_DISPOSABLE
    go test ./internal/config -run '^(TestOperatorSettingsParity|TestDefaultsMatchNixOSModule)$' -count=1 -v
    runHook postCheck
  '';
  passthru = (old.passthru or { }) // {
    inherit manifest;
  };
})
