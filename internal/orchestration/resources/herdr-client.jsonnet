local ag = import 'aginctus/orchestration.libsonnet';

function(cfg)
  local requireExistingConfig =
    if std.objectHas(cfg, 'force') && cfg.force then {}
    else {
      'user.aginctus.managed': 'true',
      'user.aginctus.resource': 'infrastructure',
      'user.aginctus.role': 'herdr-client',
    };
  ag.apply('herdr-client', {
    operation: if std.objectHas(cfg, 'operation') then cfg.operation else 'upsert',
    ensureRunning: !std.objectHas(cfg, 'operation') || cfg.operation == 'upsert',
    requireExistingConfig: requireExistingConfig,
    documents: [
      {
        kind: 'instance',
        name: cfg.name,
        image: cfg.imageAlias,
        profiles: [],
        description: 'Aginctus Herdr client infrastructure',
        config: {
          'user.aginctus.managed': 'true',
          'user.aginctus.resource': 'infrastructure',
          'user.aginctus.role': 'herdr-client',
        },
        devices: {
          root: {
            type: 'disk',
            path: '/',
            pool: cfg.storagePool,
          },
          management: {
            type: 'nic',
            network: cfg.managementNetwork,
            name: 'eth0',
          },
        },
      },
    ],
  })
