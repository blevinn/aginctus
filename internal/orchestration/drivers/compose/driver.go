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
	execute func(context.Context, Config) error
}

func New() *Driver {
	return &Driver{execute: executeCompose}
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
	return d.execute(ctx, cfg)
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

	_, err = loadProject(ctx, cfg.Project, path)
	if err != nil {
		return Config{}, fmt.Errorf("validate compose project %q: %w", cfg.Project, err)
	}
	return cfg, nil
}

func executeCompose(ctx context.Context, cfg Config) error {
	path, cleanup, err := writeCompose(cfg.Compose)
	if err != nil {
		return err
	}
	defer cleanup()

	p, err := loadProject(ctx, cfg.Project, path)
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

	stack := composeclient.NewStack(c, composeclient.StackFailFast())
	stack.AddOrdered(order, resources)

	runOptions := []composeclient.Option{
		composeclient.OptionCreate(),
		composeclient.OptionNoHealthd(),
	}
	if err := stack.ForAction(composeclient.ActionEnsure).Run(ctx, composeclient.ActionEnsure, runOptions...); err != nil {
		return fmt.Errorf("ensure compose resources for %q: %w", p.Name, err)
	}
	startable := func(resource composeclient.Resource) bool { return resource.IsEnsured() }
	if err := stack.ForActionF(composeclient.ActionStart, startable).Run(ctx, composeclient.ActionStart, runOptions...); err != nil {
		return fmt.Errorf("start compose resources for %q: %w", p.Name, err)
	}
	return nil
}

func loadProject(ctx context.Context, name, path string) (*composeproject.Project, error) {
	return composeproject.New().Load(
		ctx,
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
	)
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
