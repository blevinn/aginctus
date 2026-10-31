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
  moduleDefaults = configSource { };
  # A configured MCP subtree rendered by the typed module options. The Go
  # schema is authoritative, so the rendered source must resolve to the
  # policy the schema describes.
  mcpSource = configSource {
    mcp = {
      servers.docs = {
        transport = "streamable-http";
        url = "https://docs.example.com/mcp";
        credentialRef = "docs-token";
        allowedTools = [
          "fetch"
          "search"
        ];
      };
      servers.local = {
        transport = "stdio";
        command = "/usr/bin/mcp-local";
      };
      capabilities.coding = {
        protocol = "responses";
        requires = [ "mcp-tools" ];
      };
      grants.dev = {
        capability = "coding";
        servers.docs = [ "search" ];
      };
      gateway.enabled = true;
    };
  };
  mcpFixture = pkgs.runCommand "aginctus-nixos-mcp-fixture.json" { } ''
    cp ${mcpSource} "$out"
  '';
  fixtures =
    pkgs.runCommand "aginctus-nixos-gateway-fixtures.json" { nativeBuildInputs = [ pkgs.jq ]; }
      ''
        jq -n \
          --slurpfile defaults ${moduleDefaults} \
          --slurpfile disabled ${configSource { gateway.egress.network.name = ""; }} \
          --slurpfile forwarding ${configSource { gateway.litellm.forwardClientHeaders = true; }} \
          --slurpfile models ${
            configSource {
              gateway.litellm.forwardClientHeaders = [
                "aginctus-go-pilot"
                "provider/*"
              ];
            }
          } \
          --slurpfile emptyModels ${configSource { gateway.litellm.forwardClientHeaders = [ ]; }} \
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
          '{defaults: $defaults[0], disabled: $disabled[0], forwarding: $forwarding[0], models: $models[0], emptyModels: $emptyModels[0], ranges: $ranges[0], static: $static[0]}' > "$out"
      '';
in
self.packages.${system}.aginctus.overrideAttrs (old: {
  pname = "aginctus-gateway-config-parity";
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    export AGINCTUS_NIXOS_GATEWAY_FIXTURES=${fixtures}
    export AGINCTUS_NIXOS_MODULE_DEFAULTS=${moduleDefaults}
    export AGINCTUS_NIXOS_MCP_FIXTURES=${mcpFixture}
    unset AGINCTUS_INCUS_INTEGRATION AGINCTUS_INCUS_DISPOSABLE
    go test ./internal/gateway -run '^TestNixOSGatewayConfigParity$' -count=1 -v
    go test ./internal/config -run '^TestDefaultsMatchNixOSModule$' -count=1 -v
    go test ./internal/config -run '^TestNixOSMCPConfigParity$' -count=1 -v
    runHook postCheck
  '';
  passthru = (old.passthru or { }) // {
    inherit fixtures moduleDefaults mcpFixture;
  };
})
