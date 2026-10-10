package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"syscall"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
	composedriver "github.com/blevinn/aginctus/internal/orchestration/drivers/compose"
)

type Spec struct {
	ID                 string
	Project            string
	Network            string
	NetworkIPv4Address string
	NetworkIPv4NAT     bool
	NetworkIPv4Routing bool
	NetworkIPv6Address string
	LiteLLMImage       string
	PostgresImage      string
}

func FromConfig(effective *config.Config) (Spec, error) {
	keys := []struct {
		path string
		dst  *string
	}{}

	spec := Spec{}
	keys = []struct {
		path string
		dst  *string
	}{
		{"gateway.id", &spec.ID},
		{"gateway.compose.project", &spec.Project},
		{"incus.management.network.name", &spec.Network},
		{"incus.management.network.ipv4.address", &spec.NetworkIPv4Address},
		{"incus.management.network.ipv6.address", &spec.NetworkIPv6Address},
		{"gateway.images.litellm", &spec.LiteLLMImage},
		{"gateway.images.postgres", &spec.PostgresImage},
	}

	for _, key := range keys {
		value, err := effective.String(key.path)
		if err != nil {
			return Spec{}, err
		}
		*key.dst = value
	}

	var err error
	spec.NetworkIPv4NAT, err = effective.Bool("incus.management.network.ipv4.nat")
	if err != nil {
		return Spec{}, err
	}
	spec.NetworkIPv4Routing, err = effective.Bool("incus.management.network.ipv4.routing")
	if err != nil {
		return Spec{}, err
	}

	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func (s Spec) Validate() error {
	switch {
	case s.ID == "":
		return fmt.Errorf("gateway id must not be empty")
	case s.Project == "":
		return fmt.Errorf("gateway compose project must not be empty")
	case s.Network == "":
		return fmt.Errorf("gateway management network must not be empty")
	case s.LiteLLMImage == "":
		return fmt.Errorf("LiteLLM image must not be empty")
	case s.PostgresImage == "":
		return fmt.Errorf("PostgreSQL image must not be empty")
	default:
		return nil
	}
}

var composeTemplate = template.Must(template.New("gateway-compose").Parse(`name: {{.Project}}
services:
  postgres:
    image: {{.PostgresImage}}
    environment:
      POSTGRES_DB: litellm
      POSTGRES_USER: litellm
      POSTGRES_PASSWORD: ${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}
    volumes:
      - gateway-postgres:/var/lib/postgresql/data
    networks:
      - management

  litellm:
    image: {{.LiteLLMImage}}
    depends_on:
      - postgres
    environment:
      DATABASE_URL: postgresql://litellm:${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}@postgres:5432/litellm
      LITELLM_MASTER_KEY: ${AGINCTUS_GATEWAY_MASTER_KEY}
      LITELLM_SALT_KEY: ${AGINCTUS_GATEWAY_SALT_KEY}
      STORE_MODEL_IN_DB: "True"
    networks:
      - management

volumes:
  gateway-postgres: {}

networks:
  management:
    external: true
    name: {{.Network}}
`))

func (s Spec) RenderCompose() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := composeTemplate.Execute(&out, s); err != nil {
		return "", fmt.Errorf("render gateway compose: %w", err)
	}
	return out.String(), nil
}

var requiredRuntimeEnvironment = []string{
	"AGINCTUS_GATEWAY_POSTGRES_PASSWORD",
	"AGINCTUS_GATEWAY_MASTER_KEY",
	"AGINCTUS_GATEWAY_SALT_KEY",
}

func (s Spec) OrchestrationPlan() (orchestration.Plan, error) {
	if err := s.Validate(); err != nil {
		return orchestration.Plan{}, err
	}
	generator, err := orchestration.NewGenerator()
	if err != nil {
		return orchestration.Plan{}, err
	}
	return generator.Evaluate(
		"import 'aginctus/gateway.jsonnet'",
		map[string]any{
			"id":      s.ID,
			"project": s.Project,
			"network": map[string]any{
				"name":        s.Network,
				"ipv4Address": s.NetworkIPv4Address,
				"ipv4Nat":     s.NetworkIPv4NAT,
				"ipv4Routing": s.NetworkIPv4Routing,
				"ipv6Address": s.NetworkIPv6Address,
			},
			"litellmImage":  s.LiteLLMImage,
			"postgresImage": s.PostgresImage,
		},
	)
}

func (s Spec) InitializeRuntimeEnvironment() (map[string]string, error) {
	stateDir, err := gatewayStateDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(stateDir, "aginctus", "gateway", s.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create gateway state directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("protect gateway state directory: %w", err)
	}

	// Hold a per-gateway interprocess lock through read, initialization and
	// atomic publication, so concurrent first-time callers use one credential set.
	lockFile, err := os.OpenFile(filepath.Join(dir, ".secrets.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open gateway initialization lock: %w", err)
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("lock gateway initialization: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	path := filepath.Join(dir, "secrets.json")
	values := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("decode gateway secret state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read gateway secret state: %w", err)
	}

	changed := false
	for _, name := range requiredRuntimeEnvironment {
		if values[name] != "" {
			continue
		}
		if seed, ok := os.LookupEnv(name); ok && seed != "" {
			values[name] = seed
		} else {
			generated, err := generateSecret()
			if err != nil {
				return nil, fmt.Errorf("generate %s: %w", name, err)
			}
			values[name] = generated
		}
		changed = true
	}

	if changed {
		if err := writeSecretState(path, values); err != nil {
			return nil, err
		}
	}

	result := make(map[string]string, len(requiredRuntimeEnvironment))
	for _, name := range requiredRuntimeEnvironment {
		result[name] = values[name]
	}
	return result, nil
}

func gatewayStateDir() (string, error) {
	if value := os.Getenv("XDG_STATE_HOME"); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for gateway state: %w", err)
	}
	return filepath.Join(home, ".local", "state"), nil
}

func generateSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func writeSecretState(path string, values map[string]string) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".secrets-*.json")
	if err != nil {
		return fmt.Errorf("create gateway secret state: %w", err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("protect gateway secret state: %w", err)
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(values); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode gateway secret state: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync gateway secret state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close gateway secret state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("persist gateway secret state: %w", err)
	}
	return nil
}

func (s Spec) Deploy(ctx context.Context, runtimeEnvironment map[string]string) error {
	plan, err := s.OrchestrationPlan()
	if err != nil {
		return err
	}
	engine := orchestration.NewEngine(map[string]orchestration.Driver{
		"apply":   applydriver.New(),
		"compose": composedriver.NewWithEnvironment(runtimeEnvironment),
	})
	if _, err := engine.Execute(ctx, plan, orchestration.ExecuteOptions{}); err != nil {
		return fmt.Errorf("deploy gateway orchestration: %w", err)
	}
	return nil
}
