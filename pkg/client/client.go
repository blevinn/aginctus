// Package client provides an in-process, typed Aginctus client.
// It does not expose Cobra, execute CLI commands or define Incus authority.
package client

import "github.com/blevinn/aginctus/pkg/plugin"

// PluginInventory is the minimal read-only capability required for compiled
// plugin discovery. The internal plugin registry implements this interface,
// without leaking internal package types into the public constructor.
type PluginInventory interface {
	Inventory() []plugin.Descriptor
}

// Options contains explicitly injected in-process capabilities. This is an
// incremental constructor: new domain services will be added as migrations
// establish their authority and initialization contracts.
type Options struct {
	PluginInventory       PluginInventory
	ScopedWorkloadReader  ScopedWorkloadStatusReader
	LogicalWorkloadReader LogicalWorkloadReader
	EnvironmentReader     EnvironmentReader
	ConfigSelectionReader ConfigSelectionReader
	DefaultProject        string
	DefaultEnvironmentID  string
}

// Client is safe for concurrent read operations when its injected inventory
// follows the immutable snapshot contract.
type Client struct {
	Plugins      *PluginsService
	Workloads    *WorkloadsService
	Environments *EnvironmentsService
	Config *ConfigService
}

// New constructs a client without loading ambient configuration or touching
// Incus. A nil plugin inventory represents an empty compiled selection.
func New(options Options) *Client {
	return &Client{
		Plugins:      &PluginsService{inventory: options.PluginInventory},
		Workloads:    &WorkloadsService{reader: options.ScopedWorkloadReader, logical: options.LogicalWorkloadReader},
		Environments: &EnvironmentsService{reader: options.EnvironmentReader, defaultProject: options.DefaultProject, defaultID: options.DefaultEnvironmentID},
		Config: &ConfigService{reader: options.ConfigSelectionReader},
	}
}
