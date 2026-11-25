package environment

import "errors"

// HerdrSpec pins the project-local Herdr container and retained system disk
// inputs. The volume class and name derive from the native Herdr domain
// contract. AllowStateBootstrap is intent; it neither proves absence/history
// nor selects or grants a bootstrap operation.
type HerdrSpec struct {
	InstanceName        string `json:"instanceName"`
	ImageFingerprint    string `json:"imageFingerprint"`
	StoragePool         string `json:"storagePool"`
	ManagementNetwork   string `json:"managementNetwork"`
	AllowStateBootstrap bool   `json:"allowStateBootstrap"`
}

func (s HerdrSpec) validate(d Descriptor) error {
	// C uses the instance name directly in Incus, and derives the volume name.
	if !namePattern.MatchString(s.InstanceName) || !validAcceptedHash(s.ImageFingerprint) ||
		!gatewayReference.MatchString(s.StoragePool) || !gatewayReference.MatchString(s.ManagementNetwork) || d.Binding.Project == "default" {
		return errors.New("invalid pinned Herdr instance, image or attachment")
	}
	return nil
}
