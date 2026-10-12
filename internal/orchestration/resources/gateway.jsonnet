local ag = import 'aginctus/orchestration.libsonnet';
local cfg = std.extVar('config');
local managementNetwork = import 'aginctus/management-network.jsonnet';

ag.orchestration(cfg.id, [
  managementNetwork(cfg.network),
  ag.apply('gateway-egress-network', {
    operation: 'upsert',
    rejectUnsupportedChanges: true,
    requireExistingConfig: {
      'user.aginctus.managed': 'true',
      'user.aginctus.resource': 'gateway-egress-network',
    },
    documents: [{
      kind: 'network',
      name: cfg.egressNetwork,
      networkType: 'bridge',
      config: {
        'ipv4.address': 'auto',
        'ipv4.nat': 'true',
        'ipv4.routing': 'true',
        'ipv6.address': 'none',
        'user.aginctus.managed': 'true',
        'user.aginctus.resource': 'gateway-egress-network',
      },
    }],
  }),
  ag.compose('gateway', {
    project: cfg.project,
    compose: {
      name: cfg.project,
      services: {
        postgres: {
          image: cfg.postgresImage,
          environment: {
            POSTGRES_DB: 'litellm',
            POSTGRES_USER: 'litellm',
            POSTGRES_PASSWORD: '${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}',
          },
          volumes: [
            'gateway-postgres:/var/lib/postgresql/data',
          ],
          networks: ['management'],
        },
        litellm: {
          image: cfg.litellmImage,
          depends_on: ['postgres'],
          entrypoint: ['/bin/sh', '-ec'],
          command: [
            "printf '%s\\n' 'model_list:' '  - model_name: aginctus-default' '    litellm_params:' '      model: " + cfg.providerModel + "' '      api_key: os.environ/OPENAI_API_KEY' > /tmp/aginctus-litellm.yaml\nexec litellm --config /tmp/aginctus-litellm.yaml --port 4000\n",
          ],
          environment: {
            OPENAI_API_KEY: '${AGINCTUS_GATEWAY_UPSTREAM_API_KEY}',
            DATABASE_URL: 'postgresql://litellm:${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}@postgres:5432/litellm',
            LITELLM_MASTER_KEY: '${AGINCTUS_GATEWAY_MASTER_KEY}',
            LITELLM_SALT_KEY: '${AGINCTUS_GATEWAY_SALT_KEY}',
            STORE_MODEL_IN_DB: 'True',
          },
          networks: ['management', 'egress'],
        },
      },
      volumes: {
        'gateway-postgres': {},
      },
      networks: {
        management: {
          external: true,
          name: cfg.network.name,
        },
        egress: {
          external: true,
          name: cfg.egressNetwork,
        },
      },
    },
  }),
])
