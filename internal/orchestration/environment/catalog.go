package environment

import (
	"context"
	"errors"
	"sort"
)

// EnrollmentSource provides project-scoped records returned through an
// authenticated Incus/project state authority. Its implementations must not
// enumerate candidate files, instance labels, or arbitrary user-supplied JSON
// as evidence of enrollment.
type EnrollmentSource interface {
	Enrolled(context.Context, Binding) ([]Descriptor, error)
}

// BindingVerifier resolves the *authenticated* connection's immutable server
// identity and verifies access to the exact requested Incus project. A local
// remote alias, user-supplied serverId, or mere project flag is insufficient.
type BindingVerifier interface {
	VerifyProject(context.Context, string) (Binding, error)
}

// Catalog reads enrolled environment identities without changing state.
// Neither of its dependencies may be backed by privileged cross-project
// credentials chosen through untrusted request parameters.
type Catalog struct {
	Verifier BindingVerifier
	Source EnrollmentSource
}

// List reads records from exactly one authenticated project. Foreign, corrupt
// and duplicate records fail the entire read; no partial identities escape.
func (r Catalog) List(ctx context.Context, project string) ([]Descriptor, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	if r.Verifier == nil || r.Source == nil {
		return nil, errors.New("authenticated environment discovery unavailable")
	}
	if project == "" { return nil, errors.New("explicit Incus project required") }
	binding, err := r.Verifier.VerifyProject(ctx, project)
	if err != nil { return nil, errors.New("verified environment project unavailable") }
	if binding.Project != project || binding.ServerID == "" {
		return nil, errors.New("verified connection does not match selected project")
	}
	records, err := r.Source.Enrolled(ctx, binding)
	if err != nil { return nil, errors.New("enrolled environment inventory unavailable") }
	if records == nil { return nil, errors.New("enrolled environment inventory unavailable") }
	seen := make(map[string]bool, len(records))
	out := make([]Descriptor, 0, len(records))
	for _, d := range records {
		if err := ctx.Err(); err != nil { return nil, err }
		if err := d.Validate(); err != nil { return nil, errors.New("invalid enrolled environment record") }
		if err := d.VerifyBinding(binding); err != nil { return nil, errors.New("foreign enrolled environment record") }
		if seen[d.ID] { return nil, errors.New("duplicate environment identity in project") }
		seen[d.ID] = true
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get resolves only an immutable UUID within a verified project. Display
// names cannot authorize or disambiguate an environment.
func (r Catalog) Get(ctx context.Context, project, id string) (Descriptor, error) {
	if id == "" { return Descriptor{}, errors.New("explicit environment ID required") }
	records, err := r.List(ctx, project)
	if err != nil { return Descriptor{}, err }
	for _, d := range records {
		if d.ID == id { return d, nil }
	}
	return Descriptor{}, errors.New("enrolled environment not found")
}
