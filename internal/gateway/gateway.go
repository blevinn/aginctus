package gateway

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"text/template"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
	composedriver "github.com/blevinn/aginctus/internal/orchestration/drivers/compose"
)

type Spec struct {
	ID            string
	Project       string
	Network       string
	LiteLLMImage  string
	PostgresImage string
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
			"id":            s.ID,
			"project":       s.Project,
			"network":       s.Network,
			"litellmImage":  s.LiteLLMImage,
			"postgresImage": s.PostgresImage,
		},
	)
}

func ValidateRuntimeEnvironment() error {
	for _, name := range requiredRuntimeEnvironment {
		if value, ok := os.LookupEnv(name); !ok || value == "" {
			return fmt.Errorf("required gateway environment variable %s is not set", name)
		}
	}
	return nil
}

func (s Spec) Deploy(ctx context.Context) error {
	if err := ValidateRuntimeEnvironment(); err != nil {
		return err
	}
	plan, err := s.OrchestrationPlan()
	if err != nil {
		return err
	}
	engine := orchestration.NewEngine(map[string]orchestration.Driver{
		"compose": composedriver.New(),
	})
	if _, err := engine.Execute(ctx, plan, orchestration.ExecuteOptions{}); err != nil {
		return fmt.Errorf("deploy gateway orchestration: %w", err)
	}
	return nil
}
