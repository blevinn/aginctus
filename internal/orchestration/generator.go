package orchestration

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	jsonnet "github.com/google/go-jsonnet"
)

//go:embed resources/*.libsonnet resources/*.jsonnet
var embeddedResources embed.FS

type Generator struct {
	resources map[string]jsonnet.Contents
}

func NewGenerator() (*Generator, error) {
	resources := map[string]jsonnet.Contents{}
	err := fs.WalkDir(embeddedResources, "resources", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := embeddedResources.ReadFile(path)
		if err != nil {
			return err
		}
		name := "aginctus/" + strings.TrimPrefix(path, "resources/")
		resources[name] = jsonnet.MakeContents(string(data))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load embedded orchestration resources: %w", err)
	}
	return &Generator{resources: resources}, nil
}

func (g *Generator) WithResource(name, source string) (*Generator, error) {
	if name == "" {
		return nil, fmt.Errorf("orchestration resource name must not be empty")
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return nil, fmt.Errorf("orchestration resource name %q is not allowed", name)
	}

	resources := make(map[string]jsonnet.Contents, len(g.resources)+1)
	for key, value := range g.resources {
		resources[key] = value
	}
	if _, exists := resources[name]; exists {
		return nil, fmt.Errorf("orchestration resource %q is already registered", name)
	}
	resources[name] = jsonnet.MakeContents(source)
	return &Generator{resources: resources}, nil
}

func (g *Generator) Evaluate(source string, inputs map[string]any) (Plan, error) {
	encodedInputs, err := json.Marshal(inputs)
	if err != nil {
		return Plan{}, fmt.Errorf("encode orchestration inputs: %w", err)
	}

	vm := jsonnet.MakeVM()
	vm.Importer(&jsonnet.MemoryImporter{Data: g.resources})
	vm.ExtCode("config", string(encodedInputs))

	rendered, err := vm.EvaluateAnonymousSnippet("orchestration.jsonnet", source)
	if err != nil {
		return Plan{}, fmt.Errorf("evaluate orchestration Jsonnet: %w", err)
	}

	plan, err := ParsePlan([]byte(rendered))
	if err != nil {
		return Plan{}, fmt.Errorf("validate generated orchestration plan: %w", err)
	}
	return plan, nil
}
