[default]
[no-cd]
all: cli herdr-client opencode-workload

[no-cd]
cli:
    go build ./cmd/aginctus

[no-cd]
herdr-client:
    nix build .#herdr-client --out-link result-herdr-client

[no-cd]
opencode-workload:
    nix build .#opencode-workload --out-link result-opencode-workload
