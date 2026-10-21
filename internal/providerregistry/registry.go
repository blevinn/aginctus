package providerregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/blevinn/aginctus/internal/filestate"
)

type Provider struct {
	Type  string `json:"provider"`
	Alias string `json:"alias"`
}
type Registry struct {
	Providers []Provider `json:"providers"`
}

var safeAlias = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")

func supportedType(providerType string) bool {
	return providerType == "chatgpt" || providerType == "opencode-go"
}

// DefaultPath holds nonsecret provider identities in the current user's state.
func DefaultPath() (string, error) {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("state directory must be absolute")
	}
	return filepath.Join(root, "aginctus", "gateway", "providers.json"), nil
}
func Register(path string, p Provider) (bool, error) {
	if !supportedType(p.Type) || !safeAlias.MatchString(p.Alias) {
		return false, fmt.Errorf("invalid or unsupported provider registration")
	}
	if !filepath.IsAbs(path) {
		return false, fmt.Errorf("registry path must be absolute")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return false, fmt.Errorf("provider state directory must be private")
	}
	// Hold the kernel lock through the entire read/modify/atomic-replace.
	// File.Close also releases the lock after interrupted transactions.
	lock, err := filestate.LockRegistry(path)
	if err != nil {
		return false, fmt.Errorf("acquire provider registry write lock: %w", err)
	}
	defer lock.Close()
	reg := Registry{Providers: []Provider{}}
	data, err := filestate.ReadPrivateFile(path, 1<<20)
	if err == nil {
		if err := json.Unmarshal(data, &reg); err != nil {
			return false, fmt.Errorf("parse provider registry: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read existing provider registry: %w", err)
	}
	for _, existing := range reg.Providers {
		if existing.Alias == p.Alias {
			if existing.Type == p.Type {
				return false, nil
			}
			return false, fmt.Errorf("provider alias %q is already registered", p.Alias)
		}
	}
	reg.Providers = append(reg.Providers, p)
	data, err = json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return false, err
	}
	if err := filestate.WritePrivateFile(path, append(data, '\n')); err != nil {
		return false, err
	}
	return true, nil
}

// Load reads only nonsecret provider identities and never initializes state.
// A missing registry is treated as an empty configuration.
func Load(path string) (Registry, error) {
	empty := Registry{Providers: []Provider{}}
	if !filepath.IsAbs(path) {
		return empty, fmt.Errorf("registry path must be absolute")
	}
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return empty, fmt.Errorf("provider state directory must be private")
	}
	data, err := filestate.ReadPrivateFile(path, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("read provider registry: %w", err)
	}
	var result Registry
	if err := json.Unmarshal(data, &result); err != nil {
		return empty, fmt.Errorf("decode provider registry: %w", err)
	}
	seen := map[string]bool{}
	for _, p := range result.Providers {
		if !supportedType(p.Type) || !safeAlias.MatchString(p.Alias) || seen[p.Alias] {
			return empty, fmt.Errorf("invalid or duplicate provider registry entry")
		}
		seen[p.Alias] = true
	}
	if result.Providers == nil {
		result.Providers = []Provider{}
	}
	return result, nil
}
