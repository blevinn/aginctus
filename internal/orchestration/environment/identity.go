// Package environment defines the non-authorizing identity and Incus binding
// of an Aginctus environment. It does not resolve credentials or grant access.
package environment

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// Binding names the server and project against which an environment was
// enrolled. ServerID must come from a verified Incus server, not a local
// client alias, URL, user input alone, or the selected default remote.
type Binding struct {
	ServerID string `json:"serverId"`
	Project string `json:"project"`
}

// Descriptor identifies one independent environment lifecycle within a project.
// Name is display-only and may change without changing resource ownership.
type Descriptor struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Binding Binding `json:"binding"`
}

func (d Descriptor) Validate() error {
	if !idPattern.MatchString(d.ID) {
		return errors.New("environment id must be a canonical lowercase UUID")
	}
	if !namePattern.MatchString(d.Name) {
		return errors.New("invalid environment display name")
	}
	if strings.TrimSpace(d.Binding.ServerID) == "" || d.Binding.ServerID != strings.TrimSpace(d.Binding.ServerID) {
		return errors.New("verified Incus server identity is required")
	}
	if len(d.Binding.ServerID) > 256 || strings.ContainsAny(d.Binding.ServerID, "\x00\n\r\t") {
		return errors.New("invalid Incus server identity")
	}
	if !namePattern.MatchString(d.Binding.Project) {
		return errors.New("invalid explicit Incus project")
	}
	return nil
}

// VerifyBinding prevents a CLI connection override from silently redirecting
// an environment to a different server or project. It is not an authorization
// check: Incus must authorize each API operation with actual credentials.
func (d Descriptor) VerifyBinding(observed Binding) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if observed.ServerID != d.Binding.ServerID || observed.Project != d.Binding.Project {
		return fmt.Errorf("selected Incus connection does not match environment binding")
	}
	return nil
}

// SameEnvironment checks immutable lifecycle identity, including the verified
// server/project boundary. A matching display name alone never establishes it.
func (d Descriptor) SameEnvironment(other Descriptor) bool {
	return d.ID == other.ID && d.Binding == other.Binding
}
