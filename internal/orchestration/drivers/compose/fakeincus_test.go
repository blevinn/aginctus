// fakeIncusServer is a self-contained Incus HTTP server used only by tests in
// this package. It speaks enough of the Incus /1.0 API for the compose driver
// to run its full sequencing body without touching a real daemon. It is the
// additional seam requested by review finding B3 (#290), keeping every
// production code path that exercises the driver exercised by hermetic tests.
//
// The handler matches the path and method the upstream iclient package would
// send; it does not attempt to be a full Incus implementation.
package compose

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/lxc/incus/v7/shared/api"
)

// incusCall records one HTTP request the fake server has seen.
type incusCall struct {
	Method string
	Path   string
	Query  string
}

// fakeIncusServer is a hermetic Incus HTTP transport. It stores the project,
// instance, and image-alias state in maps keyed by Incus project name and
// instance name. The handler reports each request through Calls so tests can
// assert on the exact ordering and shape of mutations the driver performs.
type fakeIncusServer struct {
	mu sync.Mutex

	server *httptest.Server
	Calls  []incusCall

	// project is keyed by sanitized Incus project name.
	projects map[string]api.Project
	// instances is keyed by sanitized Incus project name; each value is keyed by
	// instance name.
	instances map[string]map[string]api.InstanceFull
	// imageAliases is keyed by sanitized Incus project name; each value is keyed
	// by alias name and stores the underlying fingerprint.
	imageAliases map[string]map[string]api.ImageAliasesEntry
}

// newFakeIncusServer wires up a TLS test server. URL() returns the base URL
// the caller should pass to iclient.NewConnection (with InsecureSkipVerify)
// so the hermetic test reaches this server.
func newFakeIncusServer() *fakeIncusServer {
	f := &fakeIncusServer{
		projects:     map[string]api.Project{},
		instances:    map[string]map[string]api.InstanceFull{},
		imageAliases: map[string]map[string]api.ImageAliasesEntry{},
	}
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.handle))
	return f
}

// URL returns the HTTPS base URL the test server is listening on.
func (f *fakeIncusServer) URL() string { return f.server.URL }

// Close shuts down the underlying TLS server.
func (f *fakeIncusServer) Close() { f.server.Close() }

// PutProject seeds or replaces the project state used by the next GET.
func (f *fakeIncusServer) PutProject(name string, p api.Project) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projects[name] = p
}

// PutInstance seeds or replaces an instance record returned by the next GET
// and the instance listing for the matching project.
func (f *fakeIncusServer) PutInstance(project string, inst api.InstanceFull) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, ok := f.instances[project]
	if !ok {
		bucket = map[string]api.InstanceFull{}
		f.instances[project] = bucket
	}
	bucket[inst.Name] = inst
}

// PutImageAlias seeds or replaces an image-alias entry. The fingerprint is the
// alias's resolved target; tests assert against it to verify the driver reads
// the same value the upstream Compose model expects.
func (f *fakeIncusServer) PutImageAlias(project, alias, fingerprint string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, ok := f.imageAliases[project]
	if !ok {
		bucket = map[string]api.ImageAliasesEntry{}
		f.imageAliases[project] = bucket
	}
	bucket[alias] = api.ImageAliasesEntry{
		Name: alias,
		ImageAliasesEntryPut: api.ImageAliasesEntryPut{
			Target: fingerprint,
		},
	}
}

// CallsByPath returns every recorded call whose path matches pathPrefix. It is
// safe to call from any goroutine after the test has observed completion.
func (f *fakeIncusServer) CallsByPath(pathPrefix string) []incusCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []incusCall{}
	for _, call := range f.Calls {
		if strings.HasPrefix(call.Path, pathPrefix) {
			out = append(out, call)
		}
	}
	return out
}

// handle dispatches an incoming /1.0 request. Errors mirror Incus's envelope
// shape so upstream helpers that branch on api.StatusErrorCheck behave the
// same way they would against a real daemon.
func (f *fakeIncusServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.Calls = append(f.Calls, incusCall{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
	})
	f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/1.0" || r.URL.Path == "/1.0/"):
		f.serveServerInfo(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/1.0/projects/") && len(strings.TrimPrefix(r.URL.Path, "/1.0/projects/")) > 0:
		f.serveGetProject(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/1.0/projects":
		f.serveCreateProject(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/1.0/instances":
		f.serveListInstances(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/1.0/instances/"):
		f.serveGetInstance(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/1.0/images/aliases/"):
		f.serveGetImageAlias(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/1.0/profiles/default":
		// Compose library inspects the default profile for storage pool detection.
		writeSync(w, api.Profile{Name: "default", ProfilePut: api.ProfilePut{Devices: api.DevicesMap{}}})
	case r.Method == http.MethodGet && r.URL.Path == "/1.0/storage-pools":
		writeSync(w, []api.StoragePool{{Name: "default"}})
	default:
		writeError(w, http.StatusNotFound, fmt.Sprintf("not found: %s %s", r.Method, r.URL.Path))
	}
}

func (f *fakeIncusServer) serveServerInfo(w http.ResponseWriter) {
	writeSync(w, api.Server{
		ServerUntrusted: api.ServerUntrusted{
			ServerPut: api.ServerPut{
				Config: api.ConfigMap{"core.https_address": "127.0.0.1:8443"},
			},
			APIExtensions: []string{"oci_network_config"},
			APIStatus:     "stable",
			APIVersion:    "1.0",
			Public:        false,
			Auth:          "trusted",
		},
		Environment: api.ServerEnvironment{
			ServerVersion: "7.3.0",
			Server:        "true",
		},
	})
}

func (f *fakeIncusServer) serveGetProject(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/1.0/projects/")
	f.mu.Lock()
	project, ok := f.projects[name]
	f.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("project %q not found", name))
		return
	}
	writeSync(w, project)
}

func (f *fakeIncusServer) serveCreateProject(w http.ResponseWriter, r *http.Request) {
	var post api.ProjectsPost
	if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid project create body: %v", err))
		return
	}
	f.mu.Lock()
	if _, exists := f.projects[post.Name]; exists {
		f.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf("project %q already exists", post.Name))
		return
	}
	f.projects[post.Name] = api.Project{Name: post.Name, ProjectPut: post.ProjectPut}
	f.mu.Unlock()
	writeSync(w, struct{}{})
}

func (f *fakeIncusServer) serveListInstances(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, ok := f.instances[project]
	if !ok {
		writeSync(w, []api.InstanceFull{})
		return
	}
	out := make([]api.InstanceFull, 0, len(bucket))
	for _, inst := range bucket {
		out = append(out, inst)
	}
	writeSync(w, out)
}

func (f *fakeIncusServer) serveGetInstance(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/1.0/instances/")
	project := r.URL.Query().Get("project")
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, ok := f.instances[project]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("instance %q not found in project %q", name, project))
		return
	}
	inst, ok := bucket[name]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("instance %q not found in project %q", name, project))
		return
	}
	writeSync(w, inst)
}

func (f *fakeIncusServer) serveGetImageAlias(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/1.0/images/aliases/")
	project := r.URL.Query().Get("project")
	f.mu.Lock()
	defer f.mu.Unlock()
	bucket, ok := f.imageAliases[project]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("image alias %q not found in project %q", name, project))
		return
	}
	alias, ok := bucket[name]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("image alias %q not found in project %q", name, project))
		return
	}
	writeSync(w, alias)
}

// writeSync writes a successful Incus response whose metadata is the JSON
// encoding of body.
func writeSync(w http.ResponseWriter, body any) {
	meta, err := json.Marshal(body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("encode metadata: %v", err))
		return
	}
	resp := api.Response{
		Type:       api.SyncResponse,
		Status:     "Success",
		StatusCode: http.StatusOK,
		Metadata:   meta,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeError writes an Incus-style error envelope. status is the HTTP status
// the caller wishes to report (e.g. 404).
func writeError(w http.ResponseWriter, status int, message string) {
	resp := api.Response{
		Type:  api.ErrorResponse,
		Code:  status,
		Error: message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}
