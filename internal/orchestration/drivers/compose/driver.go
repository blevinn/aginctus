package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	composeclient "github.com/lxc/incus-compose/client"
	composeproject "github.com/lxc/incus-compose/project"

	"github.com/blevinn/aginctus/internal/orchestration"
)

const (
	ownershipManagedKey  = "user.aginctus.managed"
	ownershipResourceKey = "user.aginctus.resource"
)

type Config struct {
	Project string          `json:"project"`
	Compose json.RawMessage `json:"compose"`
}

type Driver struct {
	execute     func(context.Context, Config, map[string]string) error
	environment map[string]string
}

var _ orchestration.Driver = (*Driver)(nil)

func New() *Driver {
	return &Driver{execute: executeCompose}
}

func NewWithEnvironment(environment map[string]string) *Driver {
	copied := make(map[string]string, len(environment))
	for key, value := range environment {
		copied[key] = value
	}
	return &Driver{execute: executeCompose, environment: copied}
}

func (d *Driver) SupportsDryRun() bool {
	return false
}

func (d *Driver) Validate(raw json.RawMessage) error {
	_, err := parseConfig(context.Background(), raw)
	return err
}

func (d *Driver) Execute(ctx context.Context, raw json.RawMessage, options orchestration.ExecuteOptions) error {
	if options.DryRun {
		return fmt.Errorf("compose driver does not support dry-run")
	}
	cfg, err := parseConfig(ctx, raw)
	if err != nil {
		return err
	}
	return d.execute(ctx, cfg, d.environment)
}

func parseConfig(ctx context.Context, raw json.RawMessage) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode compose driver configuration: %w", err)
	}
	if cfg.Project == "" {
		return Config{}, fmt.Errorf("compose project must not be empty")
	}
	if len(bytes.TrimSpace(cfg.Compose)) == 0 || bytes.Equal(bytes.TrimSpace(cfg.Compose), []byte("null")) {
		return Config{}, fmt.Errorf("compose configuration must not be empty")
	}

	path, cleanup, err := writeCompose(cfg.Compose)
	if err != nil {
		return Config{}, err
	}
	defer cleanup()

	_, err = loadProject(ctx, cfg.Project, path, nil)
	if err != nil {
		return Config{}, fmt.Errorf("validate compose project %q: %w", cfg.Project, err)
	}
	return cfg, nil
}


func runComposeServicesSequentially(ctx context.Context, order []string, run func(string) error) error {
	for _, service := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := run(service); err != nil {
			return err
		}
	}
	return nil
}

func executeCompose(ctx context.Context, cfg Config, environment map[string]string) error {
	path, cleanup, err := writeCompose(cfg.Compose)
	if err != nil {
		return err
	}
	defer cleanup()

	p, err := loadProject(ctx, cfg.Project, path, environment)
	if err != nil {
		return fmt.Errorf("load compose project %q: %w", cfg.Project, err)
	}

	global := composeclient.New(ctx)
	if err := global.Connect(); err != nil {
		return fmt.Errorf("connect incus-compose client: %w", err)
	}

	c, err := global.EnsureProject(
		p.Name,
		composeclient.EnsureProjectWithCreate(),
		composeclient.EnsureProjectWithConfig(p.ClientConfig.XIncus),
	)
	if err != nil {
		return fmt.Errorf("ensure compose project %q: %w", p.Name, err)
	}
	defer c.WarnError(c.Done, "compose client cleanup failed")

	if err := c.Open(); err != nil {
		return fmt.Errorf("open compose project %q: %w", p.Name, err)
	}

	resources, err := p.Resources(c)
	if err != nil {
		return fmt.Errorf("build compose resources for %q: %w", p.Name, err)
	}
	order, err := p.ServiceOrder(false)
	if err != nil {
		return fmt.Errorf("order compose services for %q: %w", p.Name, err)
	}


	runOptions := []composeclient.Option{composeclient.OptionCreate()}
	for _, action := range []composeclient.Action{composeclient.ActionEnsure, composeclient.ActionStart} {
		if err := runComposeServicesSequentially(ctx, order, func(service string) error {
			for _, resource := range resources[service] {
				if action == composeclient.ActionStart && !resource.IsEnsured() {
					continue
				}
				stack := composeclient.NewStack(c, composeclient.StackWorkers(1))
				stack.Add(resource)
				if err := stack.ForAction(action).Run(ctx, action, runOptions...); err != nil {
					return fmt.Errorf("service %q resource action %v: %w", service, action, err)
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("compose project %q action %v: %w", p.Name, action, err)
		}
	}
	return nil
}

func loadProject(ctx context.Context, name, path string, environment map[string]string) (*composeproject.Project, error) {
	options := []composeproject.LoadOption{
		composeproject.LoadFiles([]string{path}),
		composeproject.LoadName(name),
		composeproject.LoadInstanceMarks(map[string]string{
			ownershipManagedKey:  "true",
			ownershipResourceKey: "compose-instance",
		}),
		composeproject.LoadProjectMarks(map[string]string{
			ownershipManagedKey:  "true",
			ownershipResourceKey: "compose-project",
		}),
	}
	if len(environment) > 0 {
		envPath, cleanup, err := writeEnvironment(environment)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		options = append(options, composeproject.LoadEnvFiles([]string{envPath}))
	}
	return composeproject.New().Load(ctx, options...)
}

func writeEnvironment(environment map[string]string) (string, func(), error) {
	f, err := os.CreateTemp("", "aginctus-compose-env-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary compose environment: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("protect temporary compose environment: %w", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	for key, value := range environment {
		if _, err := fmt.Fprintf(f, "%s=%s\n", key, value); err != nil {
			_ = f.Close()
			cleanup()
			return "", nil, fmt.Errorf("write temporary compose environment: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary compose environment: %w", err)
	}
	return f.Name(), cleanup, nil
}

func writeCompose(contents []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "aginctus-compose-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary compose file: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(f.Name())
	}
	if _, err := f.Write(contents); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary compose file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary compose file: %w", err)
	}
	return f.Name(), cleanup, nil
}
