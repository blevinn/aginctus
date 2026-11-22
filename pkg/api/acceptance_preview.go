package api

import (
	"errors"
	"sort"
)

// PlanAcceptanceFromDiff projects a previously verified read-only
// accepted-to-configured comparison into an advisory plan. This pure
// transformation does not authenticate the diff, accept a revision,
// or grant any kind of deletion or reconciliation authority. The caller
// must obtain the diff through an authenticated project/environment reader.
func PlanAcceptanceFromDiff(diff ConfiguredAcceptedDiff) (AcceptancePlan, error) {
	if diff.APIVersion != APIVersionV1Alpha1 || diff.Kind != "ConfiguredAcceptedDiff" ||
		diff.FromLayer != "accepted" || diff.ToLayer != "configured" ||
		diff.Environment.ID == "" || diff.Environment.Binding.Project == "" ||
		diff.Environment.Binding.ServerID == "" || diff.ProposedDigest == "" ||
		diff.HasAccepted != (diff.Revision != nil) ||
		diff.NewlySelected == nil || diff.Unselected == nil || diff.Modified == nil {
		return AcceptancePlan{}, errors.New("verified accepted-to-configured diff required")
	}
	if diff.Revision != nil && (diff.Revision.ID == "" || diff.Revision.Generation == 0 || diff.Revision.CandidateDigest == "") {
		return AcceptancePlan{}, errors.New("incomplete accepted revision identity")
	}
	changes := make([]LifecycleChange, 0, len(diff.NewlySelected)+len(diff.Unselected)+len(diff.Modified)+1)
	seen := make(map[string]bool)
	for _, id := range diff.NewlySelected {
		if id == "" || seen[id] { return AcceptancePlan{}, errors.New("duplicate or empty workload identity") }
		seen[id] = true
		changes = append(changes, LifecycleChange{Resource:id, Action:"select"})
	}
	for _, id := range diff.Unselected {
		if id == "" || seen[id] { return AcceptancePlan{}, errors.New("contradictory workload identity") }
		seen[id] = true
		// An accepted-state unselection is not permission to remove an
		// existing Incus instance from the observed environment.
		changes = append(changes, LifecycleChange{Resource:id, Action:"unselect"})
	}
	for _, changed := range diff.Modified {
		if changed.ID == "" || seen[changed.ID] || len(changed.Fields) == 0 {
			return AcceptancePlan{}, errors.New("contradictory or incomplete workload modification")
		}
		seen[changed.ID] = true
		changes = append(changes, LifecycleChange{Resource:changed.ID, Action:"modify"})
	}
	if diff.DisplayNameChanged {
		changes = append(changes, LifecycleChange{Resource:"environment:"+diff.Environment.ID,Action:"rename"})
	}
	if diff.Changed != (len(changes) != 0) {
		return AcceptancePlan{}, errors.New("inconsistent configured-state change summary")
	}
	sort.Slice(changes,func(i,j int)bool{
		if changes[i].Resource == changes[j].Resource { return changes[i].Action < changes[j].Action }
		return changes[i].Resource < changes[j].Resource
	})
	plan := AcceptancePlan{
		TypeMeta:NewTypeMeta("AcceptancePlan"),
		PlanBase:PlanBase{Project:diff.Environment.Binding.Project,EnvironmentID:diff.Environment.ID,
			Changes:changes},
		CandidateDigest:diff.ProposedDigest,
	}
	if diff.Revision != nil { plan.BaseRevision = diff.Revision.ID }
	if err := plan.Validate(); err != nil { return AcceptancePlan{}, err }
	return plan,nil
}
