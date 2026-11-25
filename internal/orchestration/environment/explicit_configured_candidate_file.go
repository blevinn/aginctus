package environment

import (
	"context"
	"errors"
	"path/filepath"
)

// ExplicitConfiguredCandidateFile is a non-authorizing, read-only configured
// source. It reads exactly the caller-selected candidate file; an enrolled
// Catalog must independently verify the server/project and immutable UUID.
// NixOS/Home Manager origins do not grant additional project permissions.
// No ambient config merge or default candidate search is performed.
type ExplicitConfiguredCandidateFile struct {
	AbsolutePath string
}

func (s ExplicitConfiguredCandidateFile) ReadConfigured(ctx context.Context, enrolled Descriptor) (Candidate, error) {
	fail := func() (Candidate, error) {
		return Candidate{}, errors.New("explicit configured candidate unavailable")
	}
	if ctx == nil || ctx.Err() != nil || enrolled.Validate() != nil ||
		s.AbsolutePath == "" || !filepath.IsAbs(s.AbsolutePath) {
		return fail()
	}
	candidate, err := ReadCandidateFile(s.AbsolutePath)
	if err != nil { return fail() }
	// The binding is an identity comparison, NOT proof that the caller is
	// authorized. VerifiedConfiguredReadModel supplies that external proof.
	if _, err := SelectCandidate(candidate, enrolled, enrolled.Binding); err != nil {
		return fail()
	}
	if err := ctx.Err(); err != nil { return Candidate{}, err }
	return candidate, nil
}
