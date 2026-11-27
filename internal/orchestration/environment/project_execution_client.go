package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// ProjectExecutionSession is a sequential, authenticated coordination client.
// It carries no Incus mutation capability. Never submit an effect after a
// failed BeginEffect, even if the failure is only a lost acknowledgement.
type ProjectExecutionSession struct {
	transport   *restrictedHTTPSReadSession
	base        projectExecutionRequest
	sequence    int
	pending     bool
	synchronous bool
	correlated  bool
	unavailable bool
}

func OpenProjectExecution(ctx context.Context, config RestrictedOperatorReadConfig, project, environmentID, revisionID, workloadID, operation string) (*ProjectExecutionSession, Candidate, func(), error) {
	if !validAcceptedHash(revisionID) || !idPattern.MatchString(environmentID) || !journalIdentifier.MatchString(workloadID) {
		return nil, Candidate{}, nil, errProjectExecutionDenied
	}
	reads, closeReads, err := openProductionOperatorReads(ctx, config)
	if err != nil {
		return nil, Candidate{}, nil, err
	}
	closedOnError := true
	defer func() {
		if closedOnError {
			closeReads()
		}
	}()
	if reads.stateSession == nil {
		return nil, Candidate{}, nil, errors.New("authenticated project-state service configuration required")
	}
	desc, err := reads.AcceptedSnapshot.Catalog.Get(ctx, project, environmentID)
	if err != nil {
		return nil, Candidate{}, nil, errProjectExecutionDenied
	}
	s, err := reads.stateSession.open(ctx, desc)
	if err != nil {
		return nil, Candidate{}, nil, err
	}
	rev, candidate, err := s.ReadPublished(ctx, desc.ID)
	if err != nil || rev == nil || candidate == nil || rev.ID != revisionID {
		return nil, Candidate{}, nil, ErrAcceptedConflict
	}
	base := projectExecutionRequest{APIVersion: projectExecutionWireVersion, Revision: *rev, WorkloadID: workloadID, Operation: operation}
	base.Step = "acquire"
	id, err := sendProjectExecution(ctx, s, base)
	if err != nil {
		return nil, Candidate{}, nil, err
	}
	closedOnError = false
	base.ID = id
	return &ProjectExecutionSession{transport: s, base: base}, *candidate, closeReads, nil
}

func (s *ProjectExecutionSession) ID() string                 { return s.base.ID }
func (s *ProjectExecutionSession) Revision() AcceptedRevision { return s.base.Revision }

func (s *ProjectExecutionSession) BeginEffect(ctx context.Context, action string) error {
	if s.unavailable || s.pending {
		return ErrLocalNativeUnresolved
	}
	req := s.base
	req.Step, req.Sequence, req.Action = "begin", s.sequence+1, action
	_, err := sendProjectExecution(ctx, s.transport, req)
	if err != nil {
		s.unavailable = true
		return err
	}
	s.sequence++
	s.pending = true
	s.synchronous = s.base.Operation == "gateway-deliver" && action == "deliver" || s.base.Operation == "herdr-bootstrap" && action == "create-state"
	return nil
}

func (s *ProjectExecutionSession) MarkApplied(ctx context.Context) error {
	if s.unavailable || !s.pending || (!s.correlated && !s.synchronous) {
		return ErrLocalNativeUnresolved
	}
	req := s.base
	req.Step, req.Sequence = "applied", s.sequence
	_, err := sendProjectExecution(ctx, s.transport, req)
	if err != nil {
		s.unavailable = true
		return err
	}
	s.pending = false
	s.correlated = false
	s.synchronous = false
	return nil
}

// RecordDaemonOperation persists correlation before waiting for completion.
// A failed acknowledgement leaves the journal unresolved either way.
func (s *ProjectExecutionSession) RecordDaemonOperation(ctx context.Context, id string) error {
	if s.unavailable || !s.pending || s.correlated || s.synchronous || !daemonOperationID.MatchString(id) {
		return ErrLocalNativeUnresolved
	}
	req := s.base
	req.Step, req.Sequence, req.DaemonID = "submitted", s.sequence, id
	if _, err := sendProjectExecution(ctx, s.transport, req); err != nil {
		s.unavailable = true
		return err
	}
	s.correlated = true
	return nil
}

func (s *ProjectExecutionSession) Finish(ctx context.Context, succeeded bool) error {
	if s.unavailable || (s.pending && succeeded) {
		return ErrLocalNativeUnresolved
	}
	s.unavailable = true
	req := s.base
	req.Step, req.Sequence, req.Succeeded = "finish", s.sequence, succeeded
	_, err := sendProjectExecution(ctx, s.transport, req)
	return err
}

func sendProjectExecution(ctx context.Context, s *restrictedHTTPSReadSession, cmd projectExecutionRequest) (string, error) {
	if _, err := s.VerifiedBinding(ctx); err != nil {
		return "", errProjectExecutionDenied
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		return "", errProjectExecutionDenied
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+projectExecutionPath, bytes.NewReader(raw))
	if err != nil {
		return "", errProjectExecutionDenied
	}
	r.Header.Set("Content-Type", "application/json")
	uncertain := errors.New("native execution acknowledgement unavailable; do not retry or submit effects")
	response, err := s.client.Do(r)
	if err != nil {
		return "", uncertain
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", errProjectExecutionDenied
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return "", errors.New("native execution coordination is not enabled")
	case http.StatusConflict:
		return "", ErrLocalNativeUnresolved
	default:
		return "", uncertain
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 || response.Header.Get("Content-Type") != "application/json" || rejectCandidateDuplicateKeys(data) != nil {
		return "", uncertain
	}
	var reply projectExecutionResponse
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&reply) != nil || d.Decode(&struct{}{}) != io.EOF || reply.APIVersion != projectExecutionWireVersion ||
		!localEnvironmentAttemptID.MatchString(reply.ID) || (cmd.ID != "" && cmd.ID != reply.ID) {
		return "", uncertain
	}
	if _, err := s.VerifiedBinding(ctx); err != nil {
		return "", uncertain
	}
	return reply.ID, nil
}

// Check revalidates the live authenticated coordination session, revision and
// grant without advancing its journal. It is not a fence against Incus admins.
func (s *ProjectExecutionSession) Check(ctx context.Context) error {
	if s.unavailable {
		return ErrLocalNativeUnresolved
	}
	req := s.base
	req.Step, req.Sequence = "check", s.sequence
	_, err := sendProjectExecution(ctx, s.transport, req)
	if err != nil {
		s.unavailable = true
	}
	return err
}
