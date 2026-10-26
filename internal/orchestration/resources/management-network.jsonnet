local ag = import 'aginctus/orchestration.libsonnet';

function(cfg)
  local dhcpGateway = if cfg.ipv4DHCPGateway == '' then 'none' else cfg.ipv4DHCPGateway;
  local requireExistingConfig =
    if std.objectHas(cfg, 'force') && cfg.force then {}
    else {
      'user.aginctus.managed': 'true',
      'user.aginctus.resource': 'management-network',
    } + (if std.objectHas(cfg, 'operation') && cfg.operation == 'delete' then {}
         else { 'ipv4.dhcp.gateway': dhcpGateway });
  ag.apply('management-network', {
    operation: if std.objectHas(cfg, 'operation') then cfg.operation else 'upsert',
    rejectUnsupportedChanges: true,
    requireExistingConfig: requireExistingConfig,
    documents: [
      {
        kind: 'network',
        name: cfg.name,
        networkType: 'bridge',
        config: {
          'ipv4.address': cfg.ipv4Address,
          'ipv4.nat': std.toString(cfg.ipv4Nat),
          'ipv4.routing': std.toString(cfg.ipv4Routing),
          'ipv4.dhcp.gateway': dhcpGateway,
          'ipv6.address': cfg.ipv6Address,
          'user.aginctus.managed': 'true',
          'user.aginctus.resource': 'management-network',
        },
      },
    ],
  })
