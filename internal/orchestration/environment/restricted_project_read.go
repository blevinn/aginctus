package environment

import (
	"context"
	"errors"
)

// RestrictedProjectReadSession is supplied by a future independently
// authenticated transport. Its implementations MUST bind both methods to
// the same non-administrator Incus-project principal and connection. A struct
// containing a caller-provided project is NOT an implementation of proof.
//
// The gate intentionally has no accept, grant, generic Incus SDK, or mutation
// method. It does not implement transport authentication by itself.
type RestrictedProjectReadSession interface {
	// VerifiedBinding must derive the server and authorized project from
	// independently authenticated connection evidence, not request data.
	VerifiedBinding(context.Context) (Binding, error)
	// ReadPublished must be confined by the transport to the exact verified
	// project and immutable environment ID. It must not follow request-supplied
	// alternate storage roots, projects or remote aliases.
	ReadPublished(context.Context, string) (*AcceptedRevision, *Candidate, error)
}

// ReadRestrictedProjectAccepted provides a fail-closed application boundary
// around an injected project-restricted session. It makes no claim that the
// session's peer has been verified unless its concrete transport establishes
// that fact. Revalidation detects scope changes, not in-flight revocation.
func ReadRestrictedProjectAccepted(
	ctx context.Context, session RestrictedProjectReadSession, enrolled Descriptor,
) (*AcceptedRevision, *Candidate, error) {
	if ctx == nil {
		return nil, nil, errors.New("restricted project read requires context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if session == nil || enrolled.Validate() != nil {
		return nil, nil, errors.New("restricted project session and enrolled identity required")
	}
	binding, err := session.VerifiedBinding(ctx)
	if err != nil || enrolled.VerifyBinding(binding) != nil {
		return nil, nil, errors.New("restricted project authentication unavailable")
	}
	revision, candidate, err := session.ReadPublished(ctx, enrolled.ID)
	if err != nil {
		return nil, nil, errors.New("restricted published read unavailable")
	}
	if (revision == nil) != (candidate == nil) {
		return nil, nil, errors.New("inconsistent published state presence")
	}
	if revision != nil {
		if revision.ValidateAgainst(*candidate) != nil ||
			!candidate.Environment.SameEnvironment(enrolled) {
			return nil, nil, errors.New("foreign or corrupt published revision")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	refreshed, err := session.VerifiedBinding(ctx)
	if err != nil || refreshed != binding {
		return nil, nil, errors.New("restricted project binding changed during read")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return revision, candidate, nil
}
