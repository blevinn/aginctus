// Package native separates resource intent and pure planning from Incus SDK
// observation. Plans describe changes; they never confer mutation authority.
package native

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type InstanceIdentity struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// InstanceIntent manages only explicitly supplied fields and device properties.
// Extra observed config, devices and device properties are never removed.
// ImageAlias is project-local; fingerprint resolution belongs to observation.
// This first contract supports existing workloads only, not create/remove/replace.
type InstanceIntent struct {
	Identity      InstanceIdentity
	WorkloadID    string
	Type          string
	ImageAlias    string
	EnsureRunning bool
	Description   string
	Config        map[string]string
	Devices       map[string]map[string]string
	Profiles      []string
}

func (intent InstanceIntent) Validate() error {
	if _, err := incusscope.NewProjectScope(intent.Identity.Project); err != nil {
		return fmt.Errorf("instance project must be explicit and non-default")
	}
	if !localName(intent.Identity.Name) || !localName(intent.WorkloadID) || !localName(intent.ImageAlias) {
		return fmt.Errorf("instance identity and image alias must be explicit local names")
	}
	if intent.Type != "container" && intent.Type != "virtual-machine" {
		return fmt.Errorf("unsupported instance type")
	}
	if intent.Profiles == nil {
		return fmt.Errorf("instance profiles must be explicit")
	}
	for key, value := range intent.ownership() {
		if intent.Config[key] != value {
			return fmt.Errorf("instance intent lacks ownership markers")
		}
	}
	for name, device := range intent.Devices {
		if !localName(name) || device["type"] == "" {
			return fmt.Errorf("invalid managed device intent")
		}
	}
	return nil
}

func localName(name string) bool {
	return name != "" && !strings.ContainsAny(name, ":/\\ \t\r\n\x00") && name != "." && name != ".."
}

func (intent InstanceIntent) ownership() map[string]string {
	return map[string]string{
		"user.aginctus.managed": "true", "user.aginctus.resource": "workload",
		"user.aginctus.role": "agent-workload", "user.aginctus.workload": intent.WorkloadID,
	}
}

// InstanceObservation has no SDK types and carries no execution capability.
// Existence must be proven; lookup failure is not an empty observation.
type InstanceObservation struct {
	Identity         InstanceIdentity
	Type             string
	State            string
	Description      string
	Config           map[string]string
	Devices          map[string]map[string]string
	Profiles         []string
	ImageFingerprint string
}

type Action string

const (
	Unchanged           Action = "unchanged"
	Update              Action = "update"
	ReplacementRequired Action = "replacement-required"
)

// FieldChange intentionally excludes values, hashes of secrets and SDK errors.
type FieldChange struct {
	Field string `json:"field"`
}

type InstancePlan struct {
	Identity        InstanceIdentity `json:"identity"`
	Action          Action           `json:"action"`
	Changes         []FieldChange    `json:"changes"`
	WriteAuthorized bool             `json:"writeAuthorized"`
}

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// PlanExistingInstance is pure: no clients, filesystem, secret resolution or
// mutation. Replacement-required is a blocking classification, never permission
// to recreate an instance. Persistent volumes and omitted devices are retained.
func PlanExistingInstance(intent InstanceIntent, observed InstanceObservation) (InstancePlan, error) {
	if err := intent.Validate(); err != nil {
		return InstancePlan{}, err
	}
	if intent.Identity != observed.Identity {
		return InstancePlan{}, fmt.Errorf("observed instance identity mismatch")
	}
	for key, want := range intent.ownership() {
		if observed.Config[key] != want {
			return InstancePlan{}, fmt.Errorf("existing workload ownership unverified")
		}
	}
	if deployment := observed.Config["user.aginctus.deployment"]; deployment != "" && deployment != intent.Identity.Project {
		return InstancePlan{}, fmt.Errorf("existing workload deployment mismatch")
	}
	if observed.State != "Running" && observed.State != "Stopped" {
		return InstancePlan{}, fmt.Errorf("existing workload lifecycle unverified")
	}
	if !fingerprintPattern.MatchString(observed.ImageFingerprint) || !fingerprintPattern.MatchString(observed.Config["volatile.base_image"]) {
		return InstancePlan{}, fmt.Errorf("existing workload image identity unverified")
	}
	plan := InstancePlan{Identity: intent.Identity, Action: Unchanged}
	change := func(field string, replacement bool) {
		plan.Changes = append(plan.Changes, FieldChange{Field: field})
		if replacement {
			plan.Action = ReplacementRequired
		} else if plan.Action == Unchanged {
			plan.Action = Update
		}
	}
	if intent.Type != observed.Type {
		change("type", true)
	}
	if observed.Config["volatile.base_image"] != observed.ImageFingerprint {
		change("image", true)
	}
	if !slices.Equal(intent.Profiles, observed.Profiles) {
		// Changing inherited profiles can detach storage or widen capabilities.
		change("profiles", true)
	}
	if intent.Description != observed.Description {
		change("description", false)
	}
	for _, key := range slices.Sorted(maps.Keys(intent.Config)) {
		if got, exists := observed.Config[key]; !exists || got != intent.Config[key] {
			change("config."+key, false)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(intent.Devices)) {
		for _, key := range slices.Sorted(maps.Keys(intent.Devices[name])) {
			if got, exists := observed.Devices[name][key]; !exists || got != intent.Devices[name][key] {
				// Device changes need attachment/volume/network policy before writes.
				change("devices."+name+"."+key, true)
			}
		}
	}
	if intent.EnsureRunning && observed.State == "Stopped" && plan.Action == Unchanged {
		plan.Action = Update
	}
	return plan, nil
}

// EqualObservation is intentionally strict for a read-only snapshot recheck.
// Mutating execution must independently re-observe and authorize each operation.
func EqualObservation(a, b InstanceObservation) bool { return reflect.DeepEqual(a, b) }
