package environment

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

const publicTestEnvironmentID = "11111111-1111-4111-8111-111111111111"

func TestPublicEnvironmentProjectionRequiresVerifiedIdentity(t *testing.T) {
	enrolled := Descriptor{ID: publicTestEnvironmentID, Name: "dev", Binding: Binding{ServerID: "verified-server", Project: "aginctus"}}
	selected := enrolled
	projected, err := ProjectPublicDescriptor(enrolled, selected, enrolled.Binding)
	if err != nil { t.Fatal(err) }
	if projected.Kind != "Environment" || projected.APIVersion != api.APIVersionV1Alpha1 ||
		projected.ID != publicTestEnvironmentID || projected.Binding.Project != "aginctus" {
		t.Fatalf("unexpected public identity: %#v", projected)
	}
	data, err := json.Marshal(projected)
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(data), `"serverId":"verified-server"`) ||
		!strings.Contains(string(data), `"kind":"Environment"`) ||
		strings.Contains(string(data), "credentials") {
		t.Fatalf("incorrect projection: %s", data)
	}
	// A rename is display-only; authoritative enrolled metadata wins.
	selected.Name = "renamed"
	renamed, err := ProjectPublicDescriptor(enrolled, selected, enrolled.Binding)
	if err != nil || renamed.Name != "dev" { t.Fatalf("rename: %#v %v", renamed, err) }
}

func TestPublicEnvironmentProjectionRejectsMismatchedEvidence(t *testing.T) {
	base := Descriptor{ID: publicTestEnvironmentID, Name: "dev", Binding: Binding{ServerID: "verified-server", Project: "aginctus"}}
	for _, tc := range []struct{name string; enrolled, selected Descriptor; observed Binding}{
		{"unverified server", base, base, Binding{ServerID: "alias", Project: "aginctus"}},
		{"other project", base, base, Binding{ServerID: "verified-server", Project: "other"}},
		{"other environment", base, Descriptor{ID: "22222222-2222-4222-8222-222222222222", Name:"dev", Binding:base.Binding}, base.Binding},
		{"changed binding", base, Descriptor{ID:base.ID, Name:base.Name, Binding:Binding{ServerID:"verified-server", Project:"other"}}, base.Binding},
		{"invalid enrolled id", Descriptor{ID:"dev", Name:"dev", Binding:base.Binding}, base, base.Binding},
		{"unverified blank binding", base, base, Binding{}},
	} {
		t.Run(tc.name,func(t *testing.T){
			result,err:=ProjectPublicDescriptor(tc.enrolled,tc.selected,tc.observed)
			if err==nil || result.ID!="" || result.Kind!="" {t.Fatalf("accepted invalid selection: %#v err=%v",result,err)}
		})
	}
	// Two independently enrolled environments may intentionally share a
	// project, but neither's immutable ID can select the other.
	other := base
	other.ID = "22222222-2222-4222-8222-222222222222"
	if _, err:=ProjectPublicDescriptor(other,other,other.Binding);err!=nil {t.Fatal(err)}
	if _, err:=ProjectPublicDescriptor(base,other,base.Binding);err==nil {t.Fatal("cross-environment selection allowed")}
}
