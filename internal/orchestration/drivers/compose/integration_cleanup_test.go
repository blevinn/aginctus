package compose

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lxc/incus-compose/iclient"
)

// integrationProjectCleanup removes only the unique project created by its
// test. Cleanup runs even when a test fails. --retain disables this hook.
func integrationProjectCleanup(t *testing.T, project string) {
	t.Helper()
	if os.Getenv("AGINCTUS_INCUS_RETAIN") == "1" {
		t.Logf("retaining integration project %q", project)
		return
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		connection, err := iclient.NewConnection(&iclient.ConfigRemoteInfo{
			Name: "local", Addrs: []string{"unix://"}, Protocol: "incus",
		})
		if err != nil {
			t.Errorf("connect for cleanup of project %q: %v", project, err)
			return
		}
		if err := connection.DeleteProject(ctx, project, &iclient.DeleteProjectArgs{Force: true}); err != nil {
			t.Errorf("remove integration project %q: %v", project, err)
		}
	})
}
