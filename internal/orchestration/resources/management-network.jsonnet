local ag = import 'aginctus/orchestration.libsonnet';

function(cfg)
  local requireExistingConfig =
    if std.objectHas(cfg, 'force') && cfg.force then {}
    else {
      'user.aginctus.managed': 'true',
      'user.aginctus.resource': 'management-network',
    };
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
          'ipv6.address': cfg.ipv6Address,
          'user.aginctus.managed': 'true',
          'user.aginctus.resource': 'management-network',
        },
      },
    ],
  })
