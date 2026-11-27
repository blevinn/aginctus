package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"
)

const projectExecutionPath = "/v1/native-execution"
const projectExecutionWireVersion = "aginctus.dev/project-native-execution/v1alpha1"

type projectExecutionRequest struct {
	APIVersion string           `json:"apiVersion"`
	Step       string           `json:"step"`
	Revision   AcceptedRevision `json:"revision"`
	WorkloadID string           `json:"workloadId"`
	Operation  string           `json:"operation"`
	ID         string           `json:"id,omitempty"`
	Action     string           `json:"action,omitempty"`
	Sequence   int              `json:"sequence,omitempty"`
	Succeeded  bool             `json:"succeeded,omitempty"`
	DaemonID   string           `json:"daemonOperationId,omitempty"`
}

type projectExecutionResponse struct {
	APIVersion string `json:"apiVersion"`
	ID         string `json:"id"`
}

type projectExecutionHandler struct {
	mu         sync.Mutex
	active     *projectExecution
	volume     *MountedProjectPublishedSource
	authorizer ProjectPeerAuthorizer
	catalog    Catalog
	policy     string
	closed     bool
	recoveryObserver projectRecoveryObserver
}

func (h *projectExecutionHandler) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.active != nil {
		_ = h.active.close()
		h.active = nil
	}
}

func (h *projectExecutionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	deny := func(code int) { http.Error(w, http.StatusText(code), code) }
	if r.URL.Path != projectExecutionPath || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		deny(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		deny(http.StatusMethodNotAllowed)
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
		deny(http.StatusUnauthorized)
		return
	}
	peer := r.TLS.PeerCertificates[0]
	if peer == nil || len(peer.Raw) == 0 || !peer.Equal(r.TLS.VerifiedChains[0][0]) {
		deny(http.StatusUnauthorized)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		deny(http.StatusUnsupportedMediaType)
		return
	}
	var req projectExecutionRequest
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err != nil || rejectCandidateDuplicateKeys(data) != nil || dec.Decode(&req) != nil || dec.Decode(&struct{}{}) != io.EOF ||
		req.APIVersion != projectExecutionWireVersion || !req.validStep() {
		deny(http.StatusBadRequest)
		return
	}
	if canonical, err := json.Marshal(req); err != nil || !bytes.Equal(canonical, data) {
		deny(http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	sum := sha256.Sum256(peer.Raw)
	grant := projectExecutionGrant{PeerSHA256: hex.EncodeToString(sum[:]), ServerID: req.Revision.ServerID,
		Project: req.Revision.Project, EnvironmentID: req.Revision.EnvironmentID, RevisionID: req.Revision.ID,
		WorkloadID: req.WorkloadID, Operation: req.Operation}
	verify := func() bool {
		now := time.Now()
		if ctx.Err() != nil || now.Before(peer.NotBefore) || !now.Before(peer.NotAfter) || h.volume.checkMount(h.volume.root) != nil {
			return false
		}
		binding, err := h.authorizer.AuthorizeProjectPeer(ctx, peer)
		if err != nil || binding != h.volume.binding || !grant.valid(binding) {
			return false
		}
		grants, err := readProjectExecutionPolicy(h.policy, binding)
		_, ok := grants[grant]
		return err == nil && ok
	}
	// Bound waiters and never let a disconnected request advance the journal.
	if !h.mu.TryLock() {
		deny(http.StatusConflict)
		return
	}
	defer h.mu.Unlock()
	if h.closed {
		deny(http.StatusServiceUnavailable)
		return
	}
	if !verify() {
		deny(http.StatusForbidden)
		return
	}
	desc, err := h.catalog.Get(ctx, grant.Project, grant.EnvironmentID)
	if err != nil || desc.Binding != h.volume.binding || desc.ID != grant.EnvironmentID {
		deny(http.StatusForbidden)
		return
	}
	if grant.inspection() {
		result, err := h.volume.store.inspectProjectOperations(ctx, desc, grant, req.ID)
		if err != nil || !verify() {
			deny(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(result)
		return
	}
	if req.Operation == "recover" {
		if h.active != nil || h.recoveryObserver == nil || h.volume.store.recoverProjectExecution(ctx, desc, req.Revision, grant, req.ID, h.recoveryObserver, verify) != nil {
			deny(http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(projectExecutionResponse{projectExecutionWireVersion, req.ID})
		return
	}
	if req.Step == "acquire" {
		if h.active != nil {
			deny(http.StatusConflict)
			return
		}
		e, err := h.volume.store.beginProjectExecution(ctx, desc, req.Revision, grant)
		if err != nil {
			deny(http.StatusConflict)
			return
		}
		if !verify() {
			_ = e.close()
			deny(http.StatusForbidden)
			return
		}
		h.active = e
	} else {
		e := h.active
		if e == nil || e.grant != grant || e.journal.ID() != req.ID || e.pin.revision != req.Revision {
			deny(http.StatusConflict)
			return
		}
		n := len(e.journal.op.Effects)
		switch req.Step {
		case "check":
			if req.Sequence != n || e.check(ctx) != nil || !verify() {
				deny(http.StatusConflict)
				return
			}
		case "begin":
			if req.Sequence != n+1 || e.beginEffect(ctx, req.Action, grant) != nil || !verify() {
				deny(http.StatusConflict)
				return
			}
		case "submitted":
			if req.Sequence != n || n == 0 || e.grant.synchronous(e.journal.op.Effects[n-1].Action) || e.journal.recordDaemonOperation(ctx, req.DaemonID) != nil {
				deny(http.StatusConflict)
				return
			}
		case "applied":
			if req.Sequence != n || n == 0 || (e.journal.op.Effects[n-1].DaemonID == "" && !(e.grant.Operation == "gateway-deliver" && e.journal.op.Effects[n-1].Action == "deliver")) || e.journal.MarkApplied() != nil {
				deny(http.StatusConflict)
				return
			}
		case "finish":
			if req.Sequence != n || e.journal.Finish(req.Succeeded) != nil {
				deny(http.StatusConflict)
				return
			}
			h.active = nil
			_ = e.close()
		}
	}
	id := req.ID
	if req.Step == "acquire" {
		id = h.active.journal.ID()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(projectExecutionResponse{projectExecutionWireVersion, id})
}

func (r projectExecutionRequest) validStep() bool {
	if (r.Step == "submitted" && !daemonOperationID.MatchString(r.DaemonID)) || (r.Step != "submitted" && r.DaemonID != "") {
		return false
	}
	if (projectExecutionGrant{Operation: r.Operation}).inspection() {
		return r.Step == "inspect" && (r.ID == "" || localEnvironmentAttemptID.MatchString(r.ID)) &&
			r.Action == "" && r.Sequence == 0 && !r.Succeeded && r.Revision == inspectionRevision(r.Revision)
	}
	if r.Revision.ValidateIdentity() != nil {
		return false
	}
	if r.Operation == "recover" {
		return r.Step == "recover" && localEnvironmentAttemptID.MatchString(r.ID) && r.Action == "" && r.Sequence == 0 && !r.Succeeded
	}
	switch r.Step {
	case "acquire":
		return r.ID == "" && r.Action == "" && r.Sequence == 0 && !r.Succeeded
	case "begin":
		return localEnvironmentAttemptID.MatchString(r.ID) && r.Action != "" && r.Sequence > 0 && !r.Succeeded
	case "applied", "submitted":
		return localEnvironmentAttemptID.MatchString(r.ID) && r.Action == "" && r.Sequence > 0 && !r.Succeeded
	case "check":
		return localEnvironmentAttemptID.MatchString(r.ID) && r.Action == "" && r.Sequence >= 0 && !r.Succeeded
	case "finish":
		return localEnvironmentAttemptID.MatchString(r.ID) && r.Action == "" && r.Sequence >= 0
	default:
		return false
	}
}
