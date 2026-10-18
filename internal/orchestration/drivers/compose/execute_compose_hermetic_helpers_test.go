package compose

import (
	"context"
	"testing"

	"github.com/lxc/incus-compose/iclient"
	"github.com/lxc/incus/v7/shared/api"
)

// hermeticFixture is a thin builder over fakeIncusServer that also returns
// the connection the driver should use, plus the canonical Compose YAML for
// a single alpine service. Tests that need a different config can pass their
// own document to runHermeticExecuteCompose.
type hermeticFixture struct {
	server *fakeIncusServer
	conn   *iclient.Connection
}

// newHermeticFixture wires up the fake server and a connection that points
// at it. Tests typically defer server.Close().
func newHermeticFixture(t *testing.T) *hermeticFixture {
	t.Helper()
	server := newFakeIncusServer()
	conn, err := iclient.NewConnection(&iclient.ConfigRemoteInfo{
		Name:               "fake",
		Addrs:              []string{server.URL()},
		Protocol:           "incus",
		InsecureSkipVerify: true,
	})
	if err != nil {
		server.Close()
		t.Fatalf("iclient.NewConnection: %v", err)
	}
	return &hermeticFixture{server: server, conn: conn}
}

// factory returns a closure matching executeCompose's connection factory
// signature. Tests pass it directly to executeCompose.
func (f *hermeticFixture) factory() func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
	return func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) { return f.conn, nil }
}

// seedOwnedProject populates the project, default image alias, and (when
// non-empty) an instance that the driver would find via GetInstance. It does
// NOT seed an ownership marker on the instance; tests that want a foreign
// instance pass configWithForeignInstance instead.
func (f *hermeticFixture) seedOwnedProject(t *testing.T, project string) {
	t.Helper()
	f.server.PutProject(project, api.Project{
		Name: project,
		ProjectPut: api.ProjectPut{
			Config: map[string]string{
				ownershipManagedKey:    "true",
				ownershipResourceKey:   "compose-project",
				ownershipDeploymentKey: project,
			},
		},
	})
	// Seed the image alias the upstream library uses to resolve the cached
	// image for the alpine service in defaultComposeDocument.
	f.server.PutImageAlias(project, "docker.io/library/alpine:3.21", "fprnt-alpine-321")
}

// defaultComposeDocument is the minimal Compose file the hermetic suite uses
// when it does not care about the exact image/command. Tests that exercise
// drift detection override individual services in their own document.
const defaultComposeDocument = `services:
  app:
    image: docker.io/library/alpine:3.21
    command: ["sleep", "3600"]
`

// defaultComposeInstanceConfig returns the owned-instance config the suite
// seeds when an existing instance is part of the scenario. The marker set
// matches what the driver considers an owned compose instance.
func defaultComposeInstanceConfig(project string) api.InstanceFull {
	return api.InstanceFull{
		Instance: api.Instance{
			Name: "app-1",
			Type: string(api.InstanceTypeContainer),
			InstancePut: api.InstancePut{
				Profiles: []string{"default"},
				Config: map[string]string{
					ownershipManagedKey:    "true",
					ownershipResourceKey:   "compose-instance",
					ownershipDeploymentKey: project,
				},
			},
		},
	}
}

// runHermeticExecuteCompose is the one entry point tests use to drive
// executeCompose against the fake server. It returns the error executeCompose
// produced, the call list the fake recorded, and any non-default helper the
// test wants to invoke.
//
// Tests must close the underlying server when finished. The cleanup is not
// registered automatically because some tests want to inspect Calls after the
// driver has returned.
func runHermeticExecuteCompose(t *testing.T, server *fakeIncusServer, conn *iclient.Connection, project string, compose []byte) (error, []incusCall) {
	t.Helper()
	err := executeCompose(
		context.Background(),
		Config{Project: project, Compose: compose},
		nil,
		func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) { return conn, nil },
	)
	return err, server.Calls
}

// runHermeticExecuteComposeWithConfig drives executeCompose with the supplied
// Config and optional environment. Tests that need to exercise the
// RecreateOnDrift opt-in or pass environment substitutions use this entry
// point. The environment map is forwarded to loadProject, where it is written
// to a private temp file the upstream library consumes.
func runHermeticExecuteComposeWithConfig(t *testing.T, server *fakeIncusServer, conn *iclient.Connection, cfg Config, environment map[string]string) (error, []incusCall) {
	t.Helper()
	err := executeCompose(
		context.Background(),
		cfg,
		environment,
		func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) { return conn, nil },
	)
	return err, server.Calls
}
