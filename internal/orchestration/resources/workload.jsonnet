local ag = import 'aginctus/orchestration.libsonnet';

function(cfg)
  local requireExistingConfig =
    if std.objectHas(cfg, 'force') && cfg.force then {}
    else {
      'user.aginctus.managed': 'true',
      'user.aginctus.resource': 'workload',
      'user.aginctus.workload': cfg.id,
    };
  ag.apply('workload-' + cfg.id, {
    operation: if std.objectHas(cfg, 'operation') then cfg.operation else 'upsert',
    rejectUnsupportedChanges: true,
    ensureRunning: !std.objectHas(cfg, 'operation') || cfg.operation == 'upsert',
    requireExistingConfig: requireExistingConfig,
    documents: [
      {
        kind: 'instance',
        name: cfg.name,
        image: cfg.imageAlias,
        vm: cfg.isolation == 'vm',
        profiles: [],
        description: 'Aginctus managed agent workload ' + cfg.id,
        config: {
          'user.aginctus.managed': 'true',
          'user.aginctus.resource': 'workload',
          'user.aginctus.role': 'agent-workload',
          'user.aginctus.workload': cfg.id,
          'user.aginctus.runtime': cfg.runtime,
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
