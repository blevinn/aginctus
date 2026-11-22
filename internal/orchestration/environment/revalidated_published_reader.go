package environment

import (
	"context"
	"errors"
)

// RevalidatedPublishedReader is a read-only correctness guard for a future
// project-authenticated state transport. It is deliberately NOT a source of
// Incus credentials or proof of project authorization: the injected verifier
// and source must independently enforce actual restricted-principal access.
//
// The second verification detects a changed authenticated project binding
// during a read, but cannot substitute for revocation enforcement inside the
// physical service. No privileged admin socket, project fallback, mutation or
// new network listener is introduced.
type RevalidatedPublishedReader struct {
	Catalog Catalog
	Source  PublishedAcceptedSource
}

// Read performs exact enrolled-environment lookup, reads one accepted record,
// then rechecks the independently observed connection scope. A nil revision
// represents absence ONLY after a successful enrolled and source read.
func (r RevalidatedPublishedReader) Read(
	ctx context.Context, project, environmentID string,
) (*AcceptedRevision, *Candidate, error) {
	if ctx == nil {
		return nil, nil, errors.New("accepted read requires context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if r.Catalog.Verifier == nil || r.Catalog.Source == nil || r.Source == nil ||
		project == "" || !idPattern.MatchString(environmentID) {
		return nil, nil, errors.New("verified project and environment reader required")
	}
	desc, err := r.Catalog.Get(ctx, project, environmentID)
	if err != nil {
		return nil, nil, errors.New("verified environment selection unavailable")
	}
	revision, candidate, err := r.Source.ReadAccepted(ctx, desc)
	if err != nil {
		return nil, nil, errors.New("accepted source read unavailable")
	}
	if (revision == nil) != (candidate == nil) {
		return nil, nil, errors.New("inconsistent accepted source presence")
	}
	if revision != nil {
		if err := revision.ValidateAgainst(*candidate); err != nil ||
			!candidate.Environment.SameEnvironment(desc) {
			return nil, nil, errors.New("foreign or corrupt accepted record")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	// Reobserve the exact project after the source read so a connection
	// retarget cannot be mistaken for a successful scoped read. Only a real
	// restricted-principal verifier can make this meaningful evidence.
	observed, err := r.Catalog.Verifier.VerifyProject(ctx, project)
	if err != nil || observed != desc.Binding {
		return nil, nil, errors.New("project binding changed during accepted read")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return revision, candidate, nil
}
