package native

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	incus "github.com/lxc/incus/v7/client"
)

// Action is the reconciliation outcome for one resource.
type Action string

const (
	// Unchanged means the observed resource already satisfies the intent.
	Unchanged Action = "unchanged"
	// Update means the observed resource is owned and can be changed in place.
	Update Action = "update"
	// ReplacementRequired means the drift cannot be repaired in place.
	ReplacementRequired Action = "replacement-required"
	// Create means the resource is absent and must be created.
	Create Action = "create"
	// Delete means the resource is present and must be removed.
	Delete Action = "delete"
)

// FieldChange intentionally excludes values, hashes of secrets and SDK errors.
type FieldChange struct {
	Field string `json:"field"`
}

// ReconcileError reports the stage that failed and whether a mutation may
// have reached Incus. Incus operations are not transactional, so a failure
// never implies rollback and never reports success.
type ReconcileError struct {
	Stage     string
	Mutated   bool
	Uncertain bool
	cause     error
}

func (err *ReconcileError) Error() string {
	return "native reconciliation failed at " + err.Stage +
		"; inspect exact resource state before retry; no automatic rollback"
}

func (err *ReconcileError) Unwrap() error { return err.cause }

// localTransport refuses any client that is not bound to the local Incus
// Unix socket and to exactly the selected project. A remote daemon, an
// ambient project or a missing socket must fail before any mutation.
func localTransport(info *incus.ConnectionInfo, project string) error {
	if info == nil || info.Protocol != "incus" || !filepath.IsAbs(info.SocketPath) || info.Project != project {
		return fmt.Errorf("native reconciliation requires the local Incus socket and the selected project")
	}
	return nil
}

// connectLocal opens a bounded local Incus connection bound to one project.
// The caller must hold the returned cancel function for as long as the
// connection is used.
func connectLocal(ctx context.Context, project string) (incus.InstanceServer, context.CancelFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	server, err := incus.ConnectIncusUnixWithContext(ctx, "", &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		cancel()
		return nil, nil, observationFailure(ctx, "local Incus observation unavailable")
	}
	return server.UseProject(project), cancel, nil
}
