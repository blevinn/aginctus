package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// PromoteConfiguredCandidate uses the same pinned Incus and project-state
// identities as plan-accept, but requires an explicit compare-and-swap token.
// No infrastructure effect is exposed by the state service.
func PromoteConfiguredCandidate(ctx context.Context, configPath, project, id, expectedID string) (AcceptedRevision, error) {
	if !idPattern.MatchString(id) || (expectedID != "genesis" && !validAcceptedHash(expectedID)) {
		return AcceptedRevision{}, errors.New("explicit environment UUID and expected revision (or genesis) required")
	}
	reads, closeReads, err := OpenProductionOperatorReads(ctx, configPath)
	if err != nil {
		return AcceptedRevision{}, err
	}
	defer closeReads()
	if reads.stateSession == nil {
		return AcceptedRevision{}, errors.New("authenticated project-state service configuration required")
	}
	desc, err := reads.AcceptedSnapshot.Catalog.Get(ctx, project, id)
	if err != nil {
		return AcceptedRevision{}, errors.New("verified promotion environment unavailable")
	}
	candidate, err := reads.configuredSource.ReadConfigured(ctx, desc)
	if err != nil {
		return AcceptedRevision{}, errors.New("configured promotion candidate unavailable")
	}
	s, err := reads.stateSession.open(ctx, desc)
	if err != nil {
		return AcceptedRevision{}, err
	}
	return s.promote(ctx, candidate, expectedID)
}

func (s *restrictedHTTPSReadSession) promote(ctx context.Context, c Candidate, expectedID string) (AcceptedRevision, error) {
	if c.Environment.Binding != s.binding {
		return AcceptedRevision{}, errors.New("candidate is outside the verified project")
	}
	id := c.Environment.ID
	parent, prior, err := s.ReadPublished(ctx, id)
	if err != nil {
		return AcceptedRevision{}, err
	}
	if (parent == nil && expectedID != "genesis") || (parent != nil && parent.ID != expectedID) {
		return AcceptedRevision{}, ErrAcceptedConflict
	}
	next, err := BuildAcceptedRevision(parent, prior, c)
	if err != nil {
		return AcceptedRevision{}, err
	}
	canonical, err := c.Canonical()
	if err != nil {
		return AcceptedRevision{}, errors.New("invalid configured promotion candidate")
	}
	body, err := json.Marshal(acceptedPromotionWireRequest{
		APIVersion: acceptedPromotionWireVersion, EnvironmentID: id, Expected: parent, Candidate: canonical,
	})
	if err != nil {
		return AcceptedRevision{}, errors.New("cannot encode promotion request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/v1/promotion", bytes.NewReader(body))
	if err != nil {
		return AcceptedRevision{}, errors.New("invalid promotion endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	uncertain := errors.New("promotion acknowledgement unavailable; re-read accepted state before retrying (proposed revision " + next.ID + ")")
	resp, err := s.client.Do(req)
	if err != nil {
		return AcceptedRevision{}, uncertain
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusConflict:
		return AcceptedRevision{}, ErrAcceptedConflict
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return AcceptedRevision{}, errors.New("promotion is not enabled on the project-state service")
	case http.StatusUnauthorized, http.StatusForbidden:
		return AcceptedRevision{}, errors.New("promotion authority denied")
	case http.StatusOK:
	default:
		return AcceptedRevision{}, uncertain
	}
	const maxReply = 16 << 10
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReply+1))
	if err != nil || len(data) > maxReply || rejectCandidateDuplicateKeys(data) != nil || resp.Header.Get("Content-Type") != "application/json" {
		return AcceptedRevision{}, uncertain
	}
	var result acceptedPromotionWireResponse
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || d.Decode(&struct{}{}) != io.EOF || result.APIVersion != acceptedPromotionWireVersion || result.Revision != next {
		return AcceptedRevision{}, uncertain
	}
	if _, err := s.VerifiedBinding(ctx); err != nil {
		return AcceptedRevision{}, uncertain
	}
	return result.Revision, nil
}
