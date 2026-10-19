{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  configSource =
    settings:
    (nixpkgs.lib.nixosSystem {
      inherit system;
      modules = [
        self.nixosModules.default
        {
          system.stateVersion = "26.05";
          services.aginctus = {
            enable = true;
            package = pkgs.writeShellScriptBin "aginctus" "exit 0";
            inherit settings;
          };
        }
      ];
    }).config.environment.etc."aginctus/config.json".source;
  static = {
    gateway.egress = {
      network.ipv4.address = "10.42.40.1/24";
      ipv4.address = "10.42.40.25/24";
    };
    incus.management.network.ipv4.address = "10.42.0.1/24";
  };
  fixtures =
    pkgs.runCommand "aginctus-nixos-gateway-fixtures.json" { nativeBuildInputs = [ pkgs.jq ]; }
      ''
        jq -n \
          --slurpfile defaults ${configSource { }} \
          --slurpfile disabled ${configSource { gateway.egress.network.name = ""; }} \
          --slurpfile ranges ${
            configSource (
              nixpkgs.lib.recursiveUpdate static {
                gateway.egress.network.ipv4.dhcp = {
                  enabled = true;
                  ranges = "10.42.40.100-10.42.40.199";
                };
              }
            )
          } \
          --slurpfile static ${
            configSource (
              nixpkgs.lib.recursiveUpdate static {
                gateway.egress.network.ipv4.dhcp.enabled = false;
              }
            )
          } \
          '{defaults: $defaults[0], disabled: $disabled[0], ranges: $ranges[0], static: $static[0]}' > "$out"
      '';
in
self.packages.${system}.aginctus.overrideAttrs (old: {
  pname = "aginctus-gateway-config-parity";
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    export AGINCTUS_NIXOS_GATEWAY_FIXTURES=${fixtures}
    unset AGINCTUS_INCUS_INTEGRATION AGINCTUS_INCUS_DISPOSABLE
    go test ./internal/gateway -run '^TestNixOSGatewayConfigParity$' -count=1 -v
    runHook postCheck
  '';
  passthru = (old.passthru or { }) // {
    inherit fixtures;
  };
})
