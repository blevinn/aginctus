package environment

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// ConfiguredCandidateSource supplies one explicitly selected, already
// validated configured candidate. A filename, config merge or caller-supplied
// project label is not enrollment or authorization evidence.
type ConfiguredCandidateSource interface {
	ReadConfigured(context.Context, Descriptor) (Candidate, error)
}

// VerifiedConfiguredDiffReader adapts the domain accepted-state comparison to
// client.State.Diff, without inventing a privileged transport. Its verifier,
// enrollment source, published revision source and configured candidate source
// must be supplied by a trusted embedding host.
type VerifiedConfiguredDiffReader struct {
	Model      AcceptedReadModel
	Configured ConfiguredCandidateSource
}

// DiffConfiguredAccepted always describes the transition from accepted state
// to the explicitly proposed configured candidate. The method never mutates
// either layer and must not interpret "unselected" as deletion authority.
func (r VerifiedConfiguredDiffReader) DiffConfiguredAccepted(
	ctx context.Context, project, environmentID string,
) (api.ConfiguredAcceptedDiff, error) {
	if ctx == nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("state diff requires context")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredAcceptedDiff{}, err
	}
	if r.Configured == nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("verified configured candidate source unavailable")
	}

	enrolled, err := r.Model.Catalog.Get(ctx, project, environmentID)
	if err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("verified environment selection unavailable")
	}
	candidate, err := r.Configured.ReadConfigured(ctx, enrolled)
	if err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("configured candidate read unavailable")
	}
	if _, err := SelectCandidate(candidate, enrolled, enrolled.Binding); err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("configured candidate does not match enrolled environment")
	}

	diff, err := r.Model.DiffConfiguredAgainstAccepted(ctx, project, environmentID, candidate)
	if err != nil {
		return api.ConfiguredAcceptedDiff{}, errors.New("accepted-state comparison unavailable")
	}
	if diff.FromLayer != "accepted" || diff.ToLayer != "configured" ||
		diff.Environment.ID != enrolled.ID ||
		diff.Environment.Binding.ServerID != enrolled.Binding.ServerID ||
		diff.Environment.Binding.Project != enrolled.Binding.Project {
		return api.ConfiguredAcceptedDiff{}, errors.New("configured comparison returned foreign scope")
	}
	// The project binding may change while reading both sides. Re-observe it
	// before reporting any result; a physical service must enforce revocation
	// independently during its own operations.
	verified, err := r.Model.Catalog.Verifier.VerifyProject(ctx, project)
	if err != nil || verified != enrolled.Binding {
		return api.ConfiguredAcceptedDiff{}, errors.New("project binding changed during comparison")
	}
	if err := ctx.Err(); err != nil {
		return api.ConfiguredAcceptedDiff{}, err
	}
	return diff, nil
}
