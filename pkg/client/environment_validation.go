package client

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/blevinn/aginctus/pkg/api"
)

var environmentUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var environmentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// validateEnvironmentProjection checks a reader result, not caller authority.
// Authentication must already be enforced by the injected domain reader.
// Inconsistent identity or binding evidence cannot escape as a valid result.
func validateEnvironmentProjection(env api.Environment, project string) error {
	if !environmentUUIDPattern.MatchString(env.ID) || !environmentNamePattern.MatchString(env.Name) ||
		env.Binding.ServerID == "" || env.Binding.ServerID != strings.TrimSpace(env.Binding.ServerID) ||
		len(env.Binding.ServerID)>256 || strings.ContainsAny(env.Binding.ServerID,"\\x00\\n\\r\\t") ||
		env.Binding.Project != project {
		return errors.New("environment reader returned inconsistent identity or binding")
	}
	return nil
}

func validatedEnvironmentList(ctx context.Context, doc api.EnvironmentList, project string) error {
	if err := ctx.Err(); err != nil { return err }
	if doc.APIVersion != api.APIVersionV1Alpha1 || doc.Kind != "EnvironmentList" || doc.Items == nil {
		return errors.New("environment reader returned invalid list document")
	}
	seen := make(map[string]bool, len(doc.Items))
	for _, item := range doc.Items {
		if err := validateEnvironmentProjection(item, project); err != nil { return err }
		if seen[item.ID] { return errors.New("environment reader returned duplicate identity") }
		seen[item.ID] = true
	}
	return ctx.Err()
}

func validatedEnvironmentDetail(ctx context.Context, doc api.EnvironmentDetail, project, id string) error {
	if err := ctx.Err(); err != nil { return err }
	if doc.APIVersion != api.APIVersionV1Alpha1 || doc.Kind != "Environment" ||
		doc.ID != id {
		return errors.New("environment reader returned foreign resource identity")
	}
	if err := validateEnvironmentProjection(doc.Environment, project); err != nil { return err }
	return ctx.Err()
}
