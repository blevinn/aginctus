package environment

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFixtureUnixClientReadsOnlyPublishedIdentity(t *testing.T) {
	store, candidate := newLocalProjectFixture(t)
	ctx := context.Background()
	revision, err := store.CommitLocalCandidate(ctx, candidate.Environment, candidate.Environment.Binding, nil, candidate)
	if err != nil {
		t.Fatal(err)
	}
	socket := fixtureSocketPath(t, "client")
	server, err := StartLocalProjectReadOnly(ctx, store, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	result, err := ReadLocalFixtureRevisionStatus(ctx, socket, candidate.Environment)
	if err != nil || result.Revision != revision || result.EnvironmentID != candidate.Environment.ID {
		t.Fatalf("incorrect fixture read: %+v %v", result, err)
	}
	foreign := candidate.Environment
	foreign.Binding.Project = "foreign"
	if _, err := ReadLocalFixtureRevisionStatus(ctx, socket, foreign); err == nil {
		t.Fatal("accepted foreign project")
	}
	foreign = candidate.Environment
	foreign.ID = "22222222-2222-4222-8222-222222222222"
	if _, err := ReadLocalFixtureRevisionStatus(ctx, socket, foreign); err == nil {
		t.Fatal("treated unknown fixture revision as authenticated absence")
	}
	if _, err := ReadLocalFixtureRevisionStatus(ctx, "relative.sock", candidate.Environment); err == nil {
		t.Fatal("accepted ambient or relative socket")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ReadLocalFixtureRevisionStatus(cancelled, socket, candidate.Environment); err == nil {
		t.Fatal("read despite cancellation")
	}
}

func TestFixtureUnixClientRejectsCorruptAndOversizedResponses(t *testing.T) {
	candidate := candidateFixture()
	revision, err := BuildAcceptedRevision(nil, nil, candidate)
	if err != nil {
		t.Fatal(err)
	}
	nominal, err := json.Marshal(LocalFixtureRevisionStatus{EnvironmentID: candidate.Environment.ID, Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	socket := fixtureSocketPath(t, "fake")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var body atomic.Value
	body.Store(string(nominal))
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body.Load().(string)))
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	for _, bad := range []string{
		strings.Replace(string(nominal), candidate.Environment.ID, "22222222-2222-4222-8222-222222222222", 1),
		strings.Replace(string(nominal), `"revision":`, `"revision":null,"revision":`, 1),
		strings.TrimSuffix(string(nominal), "}") + `,"privileged":"yes"}`,
		"{}",
		strings.Repeat("x", maxLocalFixtureResponseBytes+1),
	} {
		body.Store(bad)
		if _, err := ReadLocalFixtureRevisionStatus(context.Background(), socket, candidate.Environment); err == nil {
			t.Fatalf("accepted malformed fixture response %q", bad[:min(len(bad), 40)])
		}
	}
	// The fixture client never interprets a local socket address as a
	// credential. No host Incus daemon is contacted by these checks.
	if _, err := os.Stat(filepath.Dir(socket)); err != nil {
		t.Fatal(err)
	}
}
