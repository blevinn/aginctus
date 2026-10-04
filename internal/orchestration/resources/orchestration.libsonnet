{
  orchestration(name, steps):: {
    apiVersion: 'aginctus.dev/v1alpha1',
    kind: 'Orchestration',
    metadata: {
      name: name,
    },
    steps: steps,
  },

  step(id, driver, configuration):: {
    id: id,
    driver: driver,
    configuration: configuration,
  },

  apply(id, configuration):: self.step(id, 'apply', configuration),
  compose(id, configuration):: self.step(id, 'compose', configuration),
}
