package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

const (
	SystemConfigPath = "/etc/aginctus/config.json"
	EnvPrefix        = "AGINCTUS_"
)

type Options struct {
	ConfigurationFile string
	Overrides         []string
}

type Config struct {
	values map[string]any
}

func (c *Config) Values() map[string]any {
	return cloneMap(c.values)
}

func (c *Config) Get(path string) (any, bool) {
	parts, err := splitPath(path)
	if err != nil {
		return nil, false
	}

	var current any = c.values
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func (c *Config) String(path string) (string, error) {
	value, ok := c.Get(path)
	if !ok {
		return "", fmt.Errorf("configuration key %q is not set", path)
	}
	typed, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("configuration key %q must be a string, got %T", path, value)
	}
	return typed, nil
}

// IncusProjectScope resolves the declarative project selection using the
// same validated identity used for read-only policy and create-only planning.
// A selected project is NOT proof that the current v0.1 lifecycle uses it.
func (c *Config) IncusProjectScope() (incusscope.ProjectScope, error) {
	name, err := c.String("incus.project")
	if err != nil {
		return incusscope.ProjectScope{}, fmt.Errorf("incus.project: %w", err)
	}
	scope, err := incusscope.NewProjectScope(name)
	if err != nil {
		return incusscope.ProjectScope{}, fmt.Errorf("incus.project: %w", err)
	}
	return scope, nil
}

func (c *Config) Bool(path string) (bool, error) {
	value, ok := c.Get(path)
	if !ok {
		return false, fmt.Errorf("configuration key %q is not set", path)
	}
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("configuration key %q must be a boolean, got %T", path, value)
	}
	return typed, nil
}

// Runtime gateway credentials are out-of-band inputs, not configuration keys.
// Reject them in every configuration source (including nested JSON objects),
// while leaving standalone runtime environment variables to their consumer.
func isRuntimeSecretPath(path string) bool {
	switch strings.ToLower(path) {
	case "gateway.postgres.password", "gateway.master.key", "gateway.salt.key":
		return true
	default:
		return false
	}
}

// rejectRuntimeSecretValues examines a prospective configuration subtree
// before any merge or assignment. It recognizes both nested JSON objects and
// dotted JSON keys, irrespective of key case, without printing secret values.
func rejectRuntimeSecretValues(path string, value any) error {
	if isRuntimeSecretPath(path) {
		return fmt.Errorf("runtime gateway secret keys cannot be set through configuration")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for key, child := range object {
		nested := key
		if path != "" {
			nested = path + "." + key
		}
		if err := rejectRuntimeSecretValues(nested, child); err != nil {
			return err
		}
	}
	return nil
}

type Loader struct {
	SystemPath  string
	UserPath    string
	Environment []string
}

func NewLoader() *Loader {
	userPath := ""
	if dir, err := os.UserConfigDir(); err == nil {
		userPath = filepath.Join(dir, "aginctus", "config.json")
	}

	return &Loader{
		SystemPath:  SystemConfigPath,
		UserPath:    userPath,
		Environment: os.Environ(),
	}
}

func (l *Loader) Load(options Options) (*Config, error) {
	values := cloneMap(defaults)

	if err := mergeOptionalFile(values, l.SystemPath); err != nil {
		return nil, fmt.Errorf("load system configuration: %w", err)
	}
	if err := mergeOptionalFile(values, l.UserPath); err != nil {
		return nil, fmt.Errorf("load user configuration: %w", err)
	}
	if err := mergeEnvironment(values, l.Environment); err != nil {
		return nil, fmt.Errorf("load environment configuration: %w", err)
	}
	if options.ConfigurationFile != "" {
		if err := mergeRequiredFile(values, options.ConfigurationFile); err != nil {
			return nil, fmt.Errorf("load command-line configuration file: %w", err)
		}
	}
	if err := mergeOverrides(values, options.Overrides); err != nil {
		// Never echo the raw override: it may contain a credential.
		return nil, fmt.Errorf("apply command-line configuration: %w", err)
	}

	selected := &Config{values: values}
	// Validate once after all precedence layers have been applied. An
	// invalid selection must not be silently interpreted as "default".
	if _, err := selected.IncusProjectScope(); err != nil {
		return nil, err
	}
	// The MCP policy is resolved from the same fully merged selection, so
	// an invalid MCP declaration fails before any deployment operation.
	if _, err := selected.MCP(); err != nil {
		return nil, err
	}
	return selected, nil
}

var defaults = map[string]any{
	"firewall": map[string]any{
		"zone":   "aginctus-dns",
		"egress": map[string]any{"uplinkZone": "", "allowTailscaleRouting": true},
	},
	"gateway": map[string]any{
		"id":          "local",
		"credentials": map[string]any{"storage": map[string]any{"pool": "default"}},
		"egress": map[string]any{
			"network": map[string]any{"name": "aginctus-egress"},
		},
		"compose": map[string]any{
			"project": "aginctus-gateway",
		},
		"litellm": map[string]any{"forwardClientHeaders": false, "selectiveGoForwarding": false},
		"images": map[string]any{
			"litellm":  "ghcr.io/berriai/litellm:v1.103.0",
			"postgres": "docker.io/library/postgres:17-alpine",
		},
	},
	"infrastructure": map[string]any{
		"herdr": map[string]any{
			"name": "aginctus-herdr",
			"image": map[string]any{
				"alias": "aginctus-herdr-client",
			},
			"storage": map[string]any{
				"pool": "default",
			},
		},
	},
	"workloads": map[string]any{
		"dev": map[string]any{
			"instance": map[string]any{
				"name": "aginctus-dev",
			},
			"runtime": map[string]any{
				"type": "opencode",
			},
			"isolation": "container",
			"image": map[string]any{
				"alias": "aginctus-opencode-workload",
			},
			"storage": map[string]any{
				"pool": "default",
			},
		},
	},
	"incus": map[string]any{
		"project": incusscope.DefaultAginctusProject,
		"management": map[string]any{
			"network": map[string]any{
				"name": "aginctus-mgmt",
				"ipv4": map[string]any{
					"address": "auto",
					"nat":     false,
					"routing": false,
					"dhcp": map[string]any{
						"gateway": "none",
					},
				},
				"ipv6": map[string]any{
					"address": "none",
				},
			},
		},
	},
}

func mergeOptionalFile(dst map[string]any, path string) error {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	return mergeJSON(dst, data)
}

func mergeRequiredFile(dst map[string]any, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return mergeJSON(dst, data)
}

func mergeJSON(dst map[string]any, data []byte) error {
	var src map[string]any
	if err := json.Unmarshal(data, &src); err != nil {
		return err
	}
	if src == nil {
		return fmt.Errorf("configuration document must be a JSON object, not null")
	}
	if err := rejectRuntimeSecretValues("", src); err != nil {
		return err
	}
	if err := rejectMCPCredentialValues(nil, src); err != nil {
		return err
	}
	mergeSource(dst, src, nil)
	return nil
}

func mergeEnvironment(dst map[string]any, environment []string) error {
	overlay := map[string]any{}
	entries := append([]string(nil), environment...)
	sort.Strings(entries)
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, EnvPrefix) {
			continue
		}

		suffix := strings.TrimPrefix(name, EnvPrefix)
		if suffix == "" {
			continue
		}

		path := strings.ToLower(strings.ReplaceAll(suffix, "_", "."))
		if isRuntimeSecretPath(path) {
			continue
		}
		if err := setPath(overlay, path, parseValue(value)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	mergeSource(dst, overlay, nil)
	return nil
}

func mergeOverrides(dst map[string]any, overrides []string) error {
	overlay := map[string]any{}
	for _, override := range overrides {
		path, value, ok := strings.Cut(override, "=")
		if !ok {
			return fmt.Errorf("expected path=value")
		}
		if err := setPath(overlay, path, parseValue(value)); err != nil {
			return err
		}
	}
	mergeSource(dst, overlay, nil)
	return nil
}

func parseValue(value string) any {
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err == nil {
		return parsed
	}
	return value
}

func setPath(dst map[string]any, path string, value any) error {
	parts, err := splitPath(path)
	if err != nil {
		return err
	}
	if err := rejectRuntimeSecretValues(strings.Join(parts, "."), value); err != nil {
		return err
	}
	if err := rejectMCPCredentialValues(parts, value); err != nil {
		return err
	}

	current := dst
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func splitPath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("configuration path is empty")
	}

	parts := strings.Split(path, ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil, fmt.Errorf("configuration path %q contains an empty component", path)
		}
	}
	return parts, nil
}

// Atomic configuration paths replace their complete subtree when a source
// supplies them. This keeps policy registries from being assembled out of
// unrelated lower-priority stores while preserving ordinary deep-merge
// behavior everywhere else.
var atomicConfigPaths = [][]string{{"mcp"}}

func mergeSource(dst, src map[string]any, prefix []string) {
	for key, value := range src {
		path := append(append([]string(nil), prefix...), key)
		if isAtomicConfigPath(path) {
			dst[key] = cloneValue(value)
			continue
		}
		srcMap, srcIsMap := value.(map[string]any)
		dstMap, dstIsMap := dst[key].(map[string]any)
		if srcIsMap && dstIsMap {
			mergeSource(dstMap, srcMap, path)
			continue
		}
		dst[key] = cloneValue(value)
	}
}

func isAtomicConfigPath(path []string) bool {
	for _, atomicPath := range atomicConfigPaths {
		if len(path) != len(atomicPath) {
			continue
		}
		matches := true
		for index := range path {
			if path[index] != atomicPath[index] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = cloneValue(value)
	}
	return dst
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return typed
	}
}
