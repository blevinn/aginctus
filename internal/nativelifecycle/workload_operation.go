package nativelifecycle

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type trackedWorkloadClient struct {
	native.ProjectInstanceClient
	record            func(context.Context, string) error
	project, instance string
}

func (c trackedWorkloadClient) tracked(op incus.Operation, err error) (incus.Operation, error) {
	if err != nil || op == nil {
		return op, err
	}
	return trackedWorkloadOperation{Operation: op, record: c.record,
		resource: api.NewURL().Path("1.0", "instances", c.instance).Project(c.project).String()}, nil
}

func (c trackedWorkloadClient) CreateInstance(p api.InstancesPost) (incus.Operation, error) {
	return c.tracked(c.ProjectInstanceClient.CreateInstance(p))
}

func (c trackedWorkloadClient) UpdateInstanceState(n string, p api.InstanceStatePut, etag string) (incus.Operation, error) {
	return c.tracked(c.ProjectInstanceClient.UpdateInstanceState(n, p, etag))
}

func (c trackedWorkloadClient) DeleteInstance(n string) (incus.Operation, error) {
	return c.tracked(c.ProjectInstanceClient.DeleteInstance(n))
}

type trackedWorkloadOperation struct {
	incus.Operation
	record   func(context.Context, string) error
	resource string
}

func (o trackedWorkloadOperation) WaitContext(ctx context.Context) error {
	meta := o.Get()
	instances := meta.Resources["instances"]
	if meta.Class != api.OperationClassTask || len(meta.Resources) != 1 || len(instances) != 1 || instances[0] != o.resource {
		return errors.New("daemon operation correlation unavailable; submission remains uncertain")
	}
	if err := o.record(ctx, meta.ID); err != nil {
		return err
	}
	return o.Operation.WaitContext(ctx)
}
