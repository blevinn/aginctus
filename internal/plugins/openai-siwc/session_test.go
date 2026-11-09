package openaisiwc

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestManagedLoginAndApplyOwnRouteReconciliation(t *testing.T) {
	f := newFixture(t)
	initial, err := json.Marshal(f.state())
	if err != nil {
		t.Fatal(err)
	}
	plan := &Plan{Providers: map[string]bool{"personal": true}, Routes: map[string]RouteIntent{"siwc-coding": {Account: "personal", Model: "fixture-model", KeyHash: "fixture-key-hash"}}}
	op := Operation{Action: "login", Account: "personal", Login: LoginOptions{OpenBrowser: f.browser(nil, "")}}
	if _, _, err := manage(context.Background(), initial, plan, op, f.owner.provider); err == nil {
		t.Fatal("login implicitly granted background consent")
	}
	op.ConsentConnectedApp, op.ConsentBackground = true, true
	updated, _, err := manage(context.Background(), initial, plan, op, f.owner.provider)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(updated, &state); err != nil {
		t.Fatal(err)
	}
	if state.Routes["siwc-coding"].Account != "personal" || state.Consents["personal"].Subject != f.subject {
		t.Fatal("login did not derive the configured route and consent")
	}
	plan.Routes["siwc-coding"] = RouteIntent{Account: "personal", Model: "fixture-model", KeyHash: "rotated-key-hash"}
	updated, _, err = manage(context.Background(), updated, plan, Operation{Action: "sync"}, f.owner.provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(updated, &state); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.Routes["siwc-coding"].KeyHashes, []string{"rotated-key-hash"}) {
		t.Fatal("apply did not reconcile key rotation")
	}
	delete(plan.Routes, "siwc-coding")
	updated, _, err = manage(context.Background(), updated, plan, Operation{Action: "sync"}, f.owner.provider)
	if err != nil {
		t.Fatal(err)
	}
	state = State{}
	if err := json.Unmarshal(updated, &state); err != nil || len(state.Routes) != 0 {
		t.Fatal("apply retained a removed route")
	}
	if len(f.state().Accounts) != 0 {
		t.Fatal("managed enrollment wrote a second local credential store")
	}
}
