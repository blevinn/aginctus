# Shared representation of the normalized MCP configuration subtree.
#
# The Aginctus Go schema is authoritative. This file renders the typed
# NixOS and Home Manager settings into the shape that schema accepts and
# states the consistency rules that fail evaluation instead of producing a
# configuration the loader would reject later.
{ lib }:
let
  inherit (lib) mapAttrs mapAttrsToList optionalAttrs;

  # Emit only the fields that belong to the declared transport and omit
  # optional fields that are not set. The rendered shape carries no null for
  # a typed field, so the Go schema never has to interpret one.
  server =
    value:
    {
      inherit (value) transport;
    }
    // optionalAttrs (value.credentialRef != null) { credentialRef = value.credentialRef; }
    // optionalAttrs (value.allowedTools != null) { allowedTools = value.allowedTools; }
    // (
      if value.transport == "streamable-http" then
        optionalAttrs (value.url != null) { url = value.url; }
      else
        {
          command = value.command;
          args = value.args;
        }
    );

  capability = value: {
    inherit (value) protocol;
    requires = value.requires;
  };

  grant =
    value:
    optionalAttrs (value.capability != null) { capability = value.capability; }
    // {
      servers = value.servers;
    };

  renderMCP = mcp: {
    capabilities = mapAttrs (_: capability) mcp.capabilities;
    gateway = mcp.gateway;
    grants = mapAttrs (_: grant) mcp.grants;
    servers = mapAttrs (_: server) mcp.servers;
  };

  # The MCP subtree at its declared default. A configuration that declares
  # no MCP policy renders no MCP section at all, so an existing v0.1
  # configuration keeps its exact shape.
  defaultMCP = {
    capabilities = { };
    gateway = {
      enabled = false;
      requireAuthenticatedWorkload = true;
      upstreamTimeoutSeconds = 30;
    };
    grants = { };
    servers = { };
  };
in
rec {
  # Replace the MCP subtree of a settings set with its rendered form. A
  # configuration that declares no MCP policy renders no MCP section at
  # all, so an existing v0.1 configuration keeps its exact shape.
  renderSettings =
    settings:
    if settings.mcp == defaultMCP then
      builtins.removeAttrs settings [ "mcp" ]
    else
      settings // { mcp = renderMCP settings.mcp; };

  # Assertions that keep the rendered configuration consistent with the Go
  # schema. They fail the evaluation instead of producing a configuration
  # that the loader would reject later.
  assertions =
    optionPath: mcp:
    let
      servers = mapAttrsToList (name: server: {
        inherit name;
        inherit (server) transport url command;
      }) mcp.servers;
      remoteServers = builtins.filter (server: server.transport == "streamable-http") servers;
      localServers = builtins.filter (server: server.transport == "stdio") servers;
      missingURL = builtins.filter (server: server.url == null) remoteServers;
      missingCommand = builtins.filter (server: server.command == null) localServers;
    in
    [
      {
        assertion = mcp.gateway.requireAuthenticatedWorkload;
        message = "${optionPath}.settings.mcp.gateway.requireAuthenticatedWorkload must remain enabled; the gateway binds requests to an authenticated workload identity.";
      }
      {
        assertion = missingURL == [ ];
        message = "${optionPath}.settings.mcp.servers entries with the streamable-http transport require a url: ${
          lib.concatStringsSep ", " (map (server: server.name) missingURL)
        }";
      }
      {
        assertion = missingCommand == [ ];
        message = "${optionPath}.settings.mcp.servers entries with the stdio transport require a command: ${
          lib.concatStringsSep ", " (map (server: server.name) missingCommand)
        }";
      }
      {
        assertion = !mcp.gateway.enabled || remoteServers != [ ];
        message = "${optionPath}.settings.mcp.gateway.enabled requires at least one streamable-http server to route.";
      }
      {
        assertion = mcp.gateway.enabled || remoteServers == [ ];
        message = "${optionPath}.settings.mcp.gateway.enabled must be enabled to route the declared streamable-http servers.";
      }
    ];
}
