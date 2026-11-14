package orchestration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/pkg/orchestration"
)

func TestContainerObservationRequiresExplicitLifecycleEvidence(t *testing.T) {
	for _, state := range []orchestration.ContainerState{orchestration.ContainerMissing, orchestration.ContainerUnknown, orchestration.ContainerStopped, orchestration.ContainerRunning} {
		o := orchestration.ContainerObservation{State: state, ObservedAt: time.Now()}
		present := state == orchestration.ContainerStopped || state == orchestration.ContainerRunning
		if present {
			o.ResourceIncarnation = "resource-1"
		}
		if err := o.Validate(); err != nil {
			t.Fatalf("valid %s observation rejected: %v", state, err)
		}
		if present {
			o.ResourceIncarnation = ""
		} else {
			o.ResourceIncarnation = "invented"
		}
		if err := o.Validate(); err == nil {
			t.Fatalf("invalid %s identity accepted", state)
		}
	}
	for _, o := range []orchestration.ContainerObservation{
		{}, {State: "", ObservedAt: time.Now()}, {State: orchestration.ContainerMissing},
		{State: orchestration.ContainerRunning, ObservedAt: time.Now(), ResourceIncarnation: "raw\nvalue"},
		{State: orchestration.ContainerRunning, ObservedAt: time.Now(), ResourceIncarnation: strings.Repeat("x", 2049)},
	} {
		if err := o.Validate(); err == nil {
			t.Fatalf("invalid observation accepted: %+v", o)
		}
	}
}
