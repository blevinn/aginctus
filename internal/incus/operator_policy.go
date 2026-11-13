package incus

import (
	"errors"
	"regexp"
	"strings"
)

// A managed Linux bridge has a short kernel interface name. Deliberately
// reject comma-separated ACLs, wildcard access, URLs and untrusted paths.
// A host administrator must pre-provision this exact bridge.
var delegatedBridgeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

// ProposedRestrictedOperatorConfig is a PURE candidate for a separately
// host-approved restricted project. It does not create a project, modify an
// existing project, grant TLS trust, prove effective Incus authorization,
// establish a controller fence, or authorize Home Manager writes.
//
// Unlike DesiredConfig (the legacy unconfined project policy), this shape
// matches the narrow project features and allowlists exercised in #756.
// It must not replace DesiredConfig in existing deployments automatically.
func (scope ProjectScope) ProposedRestrictedOperatorConfig(bridge, pool string) (map[string]string, error) {
	if _, err := NewProjectScope(scope.Name); err != nil {
		return nil, errors.New("invalid project for restricted operator candidate")
	}
	if !delegatedBridgeName.MatchString(bridge) ||
		!projectNamePattern.MatchString(pool) ||
		strings.ContainsAny(bridge+pool, "*,:/\\") {
		return nil, errors.New("restricted operator requires one explicit host bridge and pool")
	}
	cfg := scope.DesiredConfig()
	cfg["restricted"] = "true"
	cfg["restricted.devices.nic"] = "managed"
	cfg["restricted.networks.access"] = bridge
	cfg["restricted.storage-pools.access"] = pool
	return cfg, nil
}

// AssessProposedRestrictedOperatorConfig is read-only policy evidence.
// "compatible" means only that the observed config contains the expected
// owner labels and narrow settings; it never authenticates the caller, checks
// effective server authorization, or permits a resource mutation.
func (scope ProjectScope) AssessProposedRestrictedOperatorConfig(observedName string, observed map[string]string, bridge, pool string) ProjectAssessment {
	base := scope.AssessProject(observedName, observed)
	if base != ProjectCompatible {
		return base
	}
	required, err := scope.ProposedRestrictedOperatorConfig(bridge, pool)
	if err != nil {
		return ProjectDrift
	}
	for key, value := range required {
		if observed[key] != value {
			return ProjectDrift
		}
	}
	// Unknown Incus project restrictions/features can alter confinement:
	// fail closed rather than silently trusting a different grant policy.
	for key := range observed {
		if strings.HasPrefix(key, "restricted.") || strings.HasPrefix(key, "features.") {
			if _, approved := required[key]; !approved {
				return ProjectDrift
			}
		}
	}
	return ProjectCompatible
}
