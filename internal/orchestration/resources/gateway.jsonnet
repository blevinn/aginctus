local ag = import 'aginctus/orchestration.libsonnet';
local cfg = std.extVar('config');
local managementNetwork = import 'aginctus/management-network.jsonnet';

ag.orchestration(cfg.id, [
  managementNetwork(cfg.network),
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
          environment: {
            DATABASE_URL: 'postgresql://litellm:${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}@postgres:5432/litellm',
            LITELLM_MASTER_KEY: '${AGINCTUS_GATEWAY_MASTER_KEY}',
            LITELLM_SALT_KEY: '${AGINCTUS_GATEWAY_SALT_KEY}',
            STORE_MODEL_IN_DB: 'True',
          },
          networks: ['management'],
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
      },
    },
  }),
])
