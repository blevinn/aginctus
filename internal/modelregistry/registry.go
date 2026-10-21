// Package modelregistry stores nonsecret, inert model alias intents.
// Registration never changes LiteLLM configuration or starts authentication.
package modelregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/providerregistry"
)

type Model struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Source   string `json:"source"`
}
type Registry struct {
	Models []Model `json:"models"`
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func validModel(m Model) bool {
	name := strings.TrimPrefix(m.Name, "chatgpt/")
	if !safeName.MatchString(name) || !safeName.MatchString(m.Provider) {
		return false
	}
	if strings.HasPrefix(m.Source, "chatgpt/") {
		return safeName.MatchString(strings.TrimPrefix(m.Source, "chatgpt/"))
	}
	_, err := gateway.OpenCodeGoModelRegistration(m.Name, m.Source)
	return err == nil
}

// DefaultPath is local nonsecret control-plane state, not a LiteLLM model config.
func DefaultPath() (string, error) {
	providerPath, err := providerregistry.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(providerPath), "models.json"), nil
}

func Load(path string) (Registry, error) {
	empty := Registry{Models: []Model{}}
	if !filepath.IsAbs(path) {
		return empty, fmt.Errorf("model registry path must be absolute")
	}
	dir := filepath.Dir(path)
	d, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("inspect model registry directory: %w", err)
	}
	if !d.IsDir() || d.Mode().Perm()&0077 != 0 {
		return empty, fmt.Errorf("model registry directory must be private")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("inspect model registry: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return empty, fmt.Errorf("model registry must be a private regular file")
	}
	fd, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return empty, fmt.Errorf("open model registry safely: %w", err)
	}
	defer fd.Close()
	opened, err := fd.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return empty, fmt.Errorf("model registry changed or is insecure")
	}
	data, err := io.ReadAll(fd)
	if err != nil {
		return empty, fmt.Errorf("read model registry: %w", err)
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return empty, fmt.Errorf("invalid model registry JSON: %w", err)
	}
	seen := make(map[string]bool)
	for _, model := range reg.Models {
		if !validModel(model) || seen[model.Name] {
			return empty, fmt.Errorf("invalid or duplicate model registry entry")
		}
		seen[model.Name] = true
	}
	if reg.Models == nil {
		reg.Models = []Model{}
	}
	return reg, nil
}

// Register persists only an alias intent. It verifies the local provider
// type and source, but neither authenticates an account nor enables a route.
func Register(path string, model Model) (bool, error) {
	if !validModel(model) || !filepath.IsAbs(path) {
		return false, fmt.Errorf("invalid model registration")
	}
	providerPath, err := providerregistry.DefaultPath()
	if err != nil {
		return false, err
	}
	providers, err := providerregistry.Load(providerPath)
	if err != nil {
		return false, fmt.Errorf("verify local provider registry: %w", err)
	}
	found := false
	for _, p := range providers.Providers {
		if p.Alias != model.Provider {
			continue
		}
		found = true
		var previewErr error
		switch p.Type {
		case "chatgpt":
			_, previewErr = gateway.ChatGPTModelRegistration(model.Name, model.Source)
		case "opencode-go":
			_, previewErr = gateway.OpenCodeGoModelRegistration(model.Name, model.Source)
		default:
			return false, fmt.Errorf("unsupported model provider type")
		}
		if previewErr != nil {
			return false, fmt.Errorf("model source is incompatible with provider type %q", p.Type)
		}
		break
	}
	if !found {
		return false, fmt.Errorf("model provider alias %q is not registered locally", model.Provider)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false, fmt.Errorf("create model state directory: %w", err)
	}
	d, err := os.Lstat(dir)
	if err != nil || !d.IsDir() || d.Mode().Perm()&0077 != 0 {
		return false, fmt.Errorf("model state directory must be private")
	}
	// Keep the advisory lock across read/modify/atomic-replace; the kernel
	// releases it on Close or process death without deleting the lock inode.
	lock, err := filestate.LockRegistry(path)
	if err != nil {
		return false, fmt.Errorf("acquire model registry write lock: %w", err)
	}
	defer lock.Close()
	reg, err := Load(path)
	if err != nil {
		return false, err
	}
	for _, existing := range reg.Models {
		if existing.Name == model.Name {
			if existing == model {
				return false, nil
			}
			return false, fmt.Errorf("model alias %q already maps to another registration", model.Name)
		}
	}
	reg.Models = append(reg.Models, model)
	payload, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode model registry: %w", err)
	}
	if err := filestate.WritePrivateFile(path, append(payload, '\n')); err != nil {
		return false, fmt.Errorf("write model registry: %w", err)
	}
	return true, nil
}
