// Package orchestration defines experimental, product-neutral contribution
// contracts. These are in-memory declarations, not executable plans, accepted
// state schemas or grants of infrastructure authority. Validation performs no I/O.
package orchestration

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Target is a capability's desired contribution. IDs and dependency references
// are local to one plugin instance; the host must qualify them with deployment
// and instance scope before assembly. A target may require no containers.
// Preconditions, postconditions and ongoing observations are distinct criteria;
// a successful operation alone does not establish convergence.
type Target struct {
	ID             string
	Capability     string
	DependsOn      []string
	Containers     []Container
	Preconditions  []Check
	Postconditions []Check
	Observations   []Check
}

// Container describes a named guest requirement, not arbitrary Incus config.
// Image selection is independent of the plugin's guest compatibility contract.
// Matching container build specifications may be supplied separately by a plugin.
type Container struct {
	ID       string
	Image    ImageReference
	Contract GuestContract
}

// ImageFormat selects the image reference vocabulary, not an executor backend.
type ImageFormat string

const (
	IncusImage ImageFormat = "incus"
	OCIImage   ImageFormat = "oci"
)

// ImageReference is a nonsecret configured pointer (alias, tag, fingerprint or
// digest). Structural validity does not prove existence or immutability. The
// host must resolve/pin identity in an authorized phase before accepting
// executable intent; this package never resolves, pulls, builds or imports images.
type ImageReference struct {
	Format  ImageFormat
	Pointer string
}

// Validate checks only the format and a bounded, whitespace-free pointer. Exact
// backend syntax, scope, provenance and compatibility require separate checks.
func (r ImageReference) Validate() error {
	if r.Format != IncusImage && r.Format != OCIImage {
		return fmt.Errorf("unknown image format")
	}
	if !safeText(r.Pointer, 2048) || strings.ContainsFunc(r.Pointer, unicode.IsSpace) {
		return fmt.Errorf("image pointer must be nonempty, bounded and whitespace-free")
	}
	return nil
}

// GuestContract identifies a plugin's expected guest protocol/version and
// human-readable requirements (such as platform, services, helpers and layout).
// These declarations are not evidence that an image satisfies the contract.
// Plugins must contribute compatibility checks for the relevant lifecycle phase.
type GuestContract struct {
	ID           string
	Version      string
	Requirements []string
}

// Validate rejects ambiguous local identities and invalid declarations. It does
// not check missing dependencies or cycles across targets, capability membership,
// image compatibility or execution authority; those require host-side assembly.
// Check IDs must be unique across all phases of one target.
func (t Target) Validate() error {
	if !identifier.MatchString(t.ID) || !identifier.MatchString(t.Capability) {
		return fmt.Errorf("target ID and capability must be valid local identifiers")
	}
	dependencies := make(map[string]bool, len(t.DependsOn))
	for _, dependency := range t.DependsOn {
		if !identifier.MatchString(dependency) || dependency == t.ID || dependencies[dependency] {
			return fmt.Errorf("invalid, self-referencing or duplicate target dependency")
		}
		dependencies[dependency] = true
	}
	containers := make(map[string]bool, len(t.Containers))
	for i, container := range t.Containers {
		if !identifier.MatchString(container.ID) || containers[container.ID] {
			return fmt.Errorf("container %d has an invalid or duplicate ID", i)
		}
		containers[container.ID] = true
		if err := container.Image.Validate(); err != nil {
			return fmt.Errorf("container %d: %w", i, err)
		}
		contract := container.Contract
		if !identifier.MatchString(contract.ID) || !safeText(contract.Version, 128) || len(contract.Requirements) == 0 {
			return fmt.Errorf("container %d requires an identified, versioned guest contract", i)
		}
		for _, requirement := range contract.Requirements {
			if !safeText(requirement, 1024) {
				return fmt.Errorf("container %d has an invalid guest requirement", i)
			}
		}
	}
	checks := make(map[string]bool)
	for _, phase := range [][]Check{t.Preconditions, t.Postconditions, t.Observations} {
		for _, check := range phase {
			if err := check.Validate(); err != nil {
				return fmt.Errorf("target check: %w", err)
			}
			if checks[check.ID] {
				return fmt.Errorf("duplicate check ID across target phases")
			}
			checks[check.ID] = true
		}
	}
	return nil
}

func safeText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit &&
		!strings.ContainsFunc(value, unicode.IsControl)
}
