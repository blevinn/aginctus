local ag = import 'aginctus/orchestration.libsonnet';

function(cfg)
  ag.apply('management-network', {
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
