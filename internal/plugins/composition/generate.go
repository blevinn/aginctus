// Package appplugins is the application-owned composition point. Product-neutral
// registries never import this package or implementations. Inclusion is not enablement.
package appplugins

//go:generate go run ../../../cmd/aginctus-plugin-gen -manifest plugins.json -out generated.go
