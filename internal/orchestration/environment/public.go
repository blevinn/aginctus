package environment

import (
	"fmt"

	"github.com/blevinn/aginctus/pkg/api"
)

// ProjectPublicDescriptor produces a public identity document only after the
// selected descriptor matches the enrolled identity and the caller supplies
// independently verified connection evidence. The two descriptors must not
// become a separate source of authorization: Incus credential checks remain
// required for all subsequent reads and writes.
func ProjectPublicDescriptor(enrolled Descriptor, selected Descriptor, verified Binding) (api.EnvironmentDetail, error) {
	if err := enrolled.Validate(); err != nil {
		return api.EnvironmentDetail{}, fmt.Errorf("invalid enrolled environment: %w", err)
	}
	if err := selected.Validate(); err != nil {
		return api.EnvironmentDetail{}, fmt.Errorf("invalid selected environment: %w", err)
	}
	if !selected.SameEnvironment(enrolled) {
		return api.EnvironmentDetail{}, fmt.Errorf("selected environment does not match enrolled identity")
	}
	if err := enrolled.VerifyBinding(verified); err != nil {
		return api.EnvironmentDetail{}, err
	}
	return api.EnvironmentDetail{
		TypeMeta: api.NewTypeMeta("Environment"),
		Environment: api.Environment{
			ID: enrolled.ID, Name: enrolled.Name,
			Binding: api.EnvironmentBinding{ServerID: enrolled.Binding.ServerID, Project: enrolled.Binding.Project},
		},
	}, nil
}
