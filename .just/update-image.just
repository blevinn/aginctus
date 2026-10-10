[no-cd]
herdr-client:
    just build herdr-client
    go run ./cmd/aginctus images update aginctus-herdr-client result-herdr-client

[no-cd]
opencode-workload:
    just build opencode-workload
    go run ./cmd/aginctus images update aginctus-opencode-workload result-opencode-workload
