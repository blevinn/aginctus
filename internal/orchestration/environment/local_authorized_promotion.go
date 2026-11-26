package environment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

var errLocalPromotionDenied = errors.New("local candidate promotion denied")

// LocalPromotionRequest is the exact operation a trusted promotion policy must
// approve. The expected revision is zero only for genesis. The digest covers
// the entire proposed candidate, not a patch or set of user-supplied claims.
type LocalPromotionRequest struct {
	Environment     Descriptor
	Expected        AcceptedRevision
	CandidateDigest string
	Candidate       Candidate
}

// PromoteLocalCandidate exercises authorized CAS on the same-UID store. The
// callback must be independently bound to the actor and obtain fresh binding
// and promotion permission on each call. This is deliberately not an HTTP or
// installed CLI write path; a caller-supplied callback is not authentication.
func (s *LocalProjectStateStore) PromoteLocalCandidate(ctx context.Context, enrolled Descriptor, expected *AcceptedRevision, proposal Candidate,
	authorize func(context.Context, LocalPromotionRequest) (Binding, error),
) (AcceptedRevision, error) {
	if ctx == nil || authorize == nil {
		return AcceptedRevision{}, errors.New("local promotion requires explicit fixture authority")
	}
	if err := ctx.Err(); err != nil {
		return AcceptedRevision{}, err
	}
	if !proposal.Environment.SameEnvironment(enrolled) || enrolled.Validate() != nil {
		return AcceptedRevision{}, ErrAcceptedConflict
	}
	body, err := proposal.Canonical()
	if err != nil {
		return AcceptedRevision{}, err
	}
	candidate, err := ParseCandidate(body)
	if err != nil {
		return AcceptedRevision{}, err
	}
	sum := sha256.Sum256(body)
	request := LocalPromotionRequest{Environment: enrolled, CandidateDigest: hex.EncodeToString(sum[:])}
	if expected != nil {
		request.Expected = *expected
		expected = &request.Expected
		if expected.ValidateIdentity() != nil || expected.EnvironmentID != enrolled.ID ||
			expected.ServerID != enrolled.Binding.ServerID || expected.Project != enrolled.Binding.Project {
			return AcceptedRevision{}, ErrAcceptedConflict
		}
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		policyInput := request
		policyCandidate, err := ParseCandidate(body)
		if err != nil {
			return err
		}
		policyInput.Candidate = policyCandidate
		binding, err := authorize(ctx, policyInput)
		if err != nil || enrolled.VerifyBinding(binding) != nil {
			return errLocalPromotionDenied
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return AcceptedRevision{}, err
	}
	return s.commitLocalCandidate(ctx, enrolled, enrolled.Binding, expected, candidate, check)
}
