package client

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/pkg/api"
)

// ScopedWorkloadStatusReader is a read-only capability. Its implementation
// must independently enforce configured-project equality and ownership
// verification. Supplying a project name alone never grants authority.
type ScopedWorkloadStatusReader interface {
	ReadScopedWorkloadStatus(context.Context, api.ScopedWorkloadStatusRequest) (api.ScopedWorkloadStatusList, error)
}

// WorkloadsService exposes only the currently implemented verified scoped
// observation; environment-scoped list/show/status require their own authority.
type WorkloadsService struct {
	reader ScopedWorkloadStatusReader
}

func (s *WorkloadsService) ScopedStatus(ctx context.Context, request api.ScopedWorkloadStatusRequest) (api.ScopedWorkloadStatusList, error) {
	if err := ctx.Err(); err != nil {
		return api.ScopedWorkloadStatusList{}, err
	}
	if request.Project == "" {
		return api.ScopedWorkloadStatusList{}, fmt.Errorf("explicit project required for scoped workload status")
	}
	if s == nil || s.reader == nil {
		return api.ScopedWorkloadStatusList{}, fmt.Errorf("scoped workload inspection not configured")
	}
	return s.reader.ReadScopedWorkloadStatus(ctx, request)
}
