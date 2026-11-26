package environment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
)

func TestProjectExecutionPolicyRejectsDuplicateAndForeignGrants(t *testing.T) {
	_, candidate := newLocalProjectFixture(t)
	binding := candidate.Environment.Binding
	g := projectExecutionGrant{
		PeerSHA256: strings.Repeat("a", 64), ServerID: binding.ServerID, Project: binding.Project,
		EnvironmentID: candidate.Environment.ID, RevisionID: strings.Repeat("b", 64), WorkloadID: "beta", Operation: "stop",
	}
	encode := func(grants ...projectExecutionGrant) []byte {
		t.Helper()
		b, err := json.Marshal(map[string]any{"apiVersion": projectExecutionPolicyVersion, "grants": grants})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	grants, err := parseProjectExecutionPolicy(encode(g), binding)
	if _, found := grants[g]; err != nil || !found {
		t.Fatalf("valid grant was not accepted: %v", err)
	}
	if _, err := parseProjectExecutionPolicy(encode(g, g), binding); err == nil {
		t.Fatal("duplicate grant accepted")
	}
	g.Project = "foreign"
	if _, err := parseProjectExecutionPolicy(encode(g), binding); err == nil {
		t.Fatal("foreign grant accepted")
	}
}

func TestProjectExecutionExcludesPeerAndRetainsUncertainty(t *testing.T) {
	ctx := context.Background()
	store, a := newLocalProjectFixture(t)
	b := a
	b.Environment.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
	commit := func(c Candidate) (AcceptedRevision, projectExecutionGrant) {
		t.Helper()
		rev, err := store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c)
		if err != nil {
			t.Fatal(err)
		}
		return rev, projectExecutionGrant{
			PeerSHA256: strings.Repeat("a", 64), ServerID: rev.ServerID, Project: rev.Project,
			EnvironmentID: rev.EnvironmentID, RevisionID: rev.ID, WorkloadID: "beta", Operation: "reconcile",
		}
	}
	aRev, aGrant := commit(a)
	bRev, bGrant := commit(b) // Both environments name the same workload instance.
	e, err := store.beginProjectExecution(ctx, a.Environment, aRev, aGrant)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	if _, err := store.beginProjectExecution(ctx, b.Environment, bRev, bGrant); !errors.Is(err, filestate.ErrRegistryBusy) {
		t.Fatalf("competing writer: %v", err)
	}
	wrong := aGrant
	wrong.PeerSHA256 = strings.Repeat("b", 64)
	if err := e.beginEffect(ctx, "create", wrong); !errors.Is(err, errProjectExecutionDenied) {
		t.Fatalf("wrong actor: %v", err)
	}
	if err := e.beginEffect(ctx, "delete", aGrant); !errors.Is(err, errProjectExecutionDenied) {
		t.Fatalf("reconcile permitted deletion: %v", err)
	}
	if err := e.beginEffect(ctx, "create", aGrant); err != nil {
		t.Fatal(err)
	}
	id := e.journal.ID()
	if err := e.close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenLocalProjectStateStore(store.root, store.binding)
	if err != nil {
		t.Fatal(err)
	}
	op, err := store.ReadLocalNativeOperation(ctx, a.Environment, id)
	if err != nil || op.Execution == nil || *op.Execution != aGrant || len(op.Effects) != 1 || op.Effects[0].Outcome != LocalNativeUncertain {
		t.Fatalf("missing durable uncertainty: %+v %v", op, err)
	}
	if _, err := store.beginProjectExecution(ctx, b.Environment, bRev, bGrant); !errors.Is(err, ErrLocalNativeUnresolved) {
		t.Fatalf("restart failed to exclude competing writer: %v", err)
	}
}
