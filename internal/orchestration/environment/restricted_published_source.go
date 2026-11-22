package environment

import (
	"context"
	"errors"
)

// RestrictedSessionFactory creates a project-restricted, independently
// authenticated read session for an enrolled environment. A concrete factory
// MUST derive the Incus principal and connection from verified transport
// evidence, never from an asserted Descriptor or caller-supplied project.
// This interface is not itself a proof of authentication.
type RestrictedSessionFactory interface {
	OpenReadSession(context.Context, Descriptor) (RestrictedProjectReadSession, error)
}

// RestrictedPublishedSource connects the existing restricted-session read gate
// to AcceptedReadModel's PublishedAcceptedSource capability. It deliberately
// contains no socket path, credentials, Incus admin client or fallback store.
// No production session factory is installed by default.
type RestrictedPublishedSource struct {
	Factory RestrictedSessionFactory
}

// ReadAccepted is strictly read-only and never fabricates a missing accepted
// revision when session creation, authentication, or record validation fails.
func (s RestrictedPublishedSource) ReadAccepted(
	ctx context.Context, enrolled Descriptor,
) (*AcceptedRevision, *Candidate, error) {
	if ctx == nil { return nil,nil,errors.New("published read requires context") }
	if err := ctx.Err(); err != nil { return nil,nil,err }
	if s.Factory == nil || enrolled.Validate() != nil {
		return nil,nil,errors.New("verified enrolled environment and read session required")
	}
	session, err := s.Factory.OpenReadSession(ctx,enrolled)
	if err != nil || session == nil {
		if ctx.Err() != nil { return nil,nil,ctx.Err() }
		return nil,nil,errors.New("restricted project read session unavailable")
	}
	rev,candidate,err := ReadRestrictedProjectAccepted(ctx,session,enrolled)
	if err != nil {
		if ctx.Err() != nil { return nil,nil,ctx.Err() }
		return nil,nil,errors.New("restricted accepted-state read unavailable")
	}
	return rev,candidate,nil
}
