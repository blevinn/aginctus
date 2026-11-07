package incus

import "fmt"

// DefaultHostProject is the Incus project that owns host-shared resources.
const DefaultHostProject = "default"

// HostSharedScope is the single documented exception to the non-default
// project rule. The Aginctus management bridge is host-owned: Aginctus
// projects disable project-local networks, so the bridge lives in the Incus
// "default" project and is referenced by name from project-scoped instances.
//
// The scope is deliberately not a general escape hatch. Only the resources
// listed in hostSharedResources may use it, and each carries its own
// ownership markers.
type HostSharedScope struct {
	project  string
	resource string
}

// hostSharedResources lists the resources allowed to use the host-shared
// scope. Adding an entry is an explicit policy decision.
var hostSharedResources = map[string]bool{
	"management-network": true,
}

// NewHostSharedScope returns the scope for a named host-shared resource.
func NewHostSharedScope(resource string) (HostSharedScope, error) {
	if !hostSharedResources[resource] {
		return HostSharedScope{}, fmt.Errorf("incus host-shared scope does not permit resource %q", resource)
	}
	return HostSharedScope{project: DefaultHostProject, resource: resource}, nil
}

// Project is the Incus project that owns the resource.
func (scope HostSharedScope) Project() string { return scope.project }

// Resource is the Aginctus resource kind carried by the ownership markers.
func (scope HostSharedScope) Resource() string { return scope.resource }

// Ownership returns the markers that identify an Aginctus-owned resource of
// this kind. They are written at creation and verified before every write.
func (scope HostSharedScope) Ownership() map[string]string {
	return map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": scope.resource,
	}
}

// OwnershipAssessment separates an owned resource from a foreign one. A
// resource that merely shares the name is never owned.
type OwnershipAssessment string

const (
	// HostOwned means every ownership marker matches exactly.
	HostOwned OwnershipAssessment = "owned"
	// HostForeign means at least one ownership marker is missing or differs.
	HostForeign OwnershipAssessment = "foreign"
)

// AssessOwnership never mutates the observed resource. Missing markers are
// foreign, not an invitation to repair.
func (scope HostSharedScope) AssessOwnership(observed map[string]string) OwnershipAssessment {
	for key, want := range scope.Ownership() {
		if observed[key] != want {
			return HostForeign
		}
	}
	return HostOwned
}
