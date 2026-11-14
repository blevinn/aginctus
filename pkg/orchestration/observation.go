package orchestration

import (
	"fmt"
	"time"
)

// ContainerState describes lifecycle only, not guest/application readiness.
type ContainerState string

const (
	ContainerMissing ContainerState = "missing"
	ContainerStopped ContainerState = "stopped"
	ContainerRunning ContainerState = "running"
	ContainerUnknown ContainerState = "unknown"
)

// ContainerObservation is a nonsecret projection from an authorized host reader.
// ResourceIncarnation identifies the observed resource, not an authority grant.
// Missing and unknown observations must not invent an incarnation.
type ContainerObservation struct {
	State               ContainerState
	ObservedAt          time.Time
	ResourceIncarnation string
}

func (o ContainerObservation) Validate() error {
	if o.ObservedAt.IsZero() {
		return fmt.Errorf("container observation requires an observation time")
	}
	switch o.State {
	case ContainerRunning, ContainerStopped:
		if !safeText(o.ResourceIncarnation, 2048) {
			return fmt.Errorf("present container requires a bounded nonsecret incarnation")
		}
	case ContainerMissing, ContainerUnknown:
		if o.ResourceIncarnation != "" {
			return fmt.Errorf("missing or unknown container cannot establish incarnation")
		}
	default:
		return fmt.Errorf("invalid container observation state")
	}
	return nil
}
