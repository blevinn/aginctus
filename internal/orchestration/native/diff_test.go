package native

import (
	"reflect"
	"testing"
)

func owned(kind Kind, config map[string]string) Observed {
	values := map[string]string{ManagedKey: "true", ResourceKey: string(kind)}
	for k, v := range config {
		values[k] = v
	}
	return Observed{Exists: true, Config: values}
}
func TestPlanActionsAndNoImplicitDeletion(t *testing.T) {
	resources := []Resource{
		{Kind: Network, Name: "new"},
		{Kind: Network, Name: "steady", Config: map[string]string{"ipv4.nat": "false"}},
		{Kind: Instance, Name: "changed", Config: map[string]string{"a": "1", "b": "2"}},
		{Kind: Volume, Name: "remove", State: Absent},
	}
	actual := map[ID]Observed{
		resources[1].ID():                 owned(Network, map[string]string{"ipv4.nat": "false", "unmanaged": "untouched"}),
		resources[2].ID():                 owned(Instance, map[string]string{"a": "0", "b": "0"}),
		resources[3].ID():                 owned(Volume, nil),
		ID{Kind: Volume, Name: "omitted"}: owned(Volume, nil),
	}
	got, err := Plan(resources, actual)
	if err != nil {
		t.Fatal(err)
	}
	actions := []Action{Create, Noop, Update, Delete}
	for i, c := range got {
		if c.Action != actions[i] {
			t.Errorf("step %d: %s", i, c.Action)
		}
	}
	if !reflect.DeepEqual(got[2].Keys, []string{"a", "b"}) {
		t.Fatalf("unstable keys: %v", got[2].Keys)
	}
	if len(got) != len(resources) {
		t.Fatal("planner acted on omitted resource")
	}
}
func TestPlanRejectsForeignAndReservedOwnership(t *testing.T) {
	r := Resource{Kind: Instance, Name: "agent"}
	_, err := Plan([]Resource{r}, map[ID]Observed{r.ID(): {Exists: true, Config: map[string]string{}}})
	if err == nil {
		t.Fatal("foreign resource accepted")
	}
	r.Config = map[string]string{ManagedKey: "true"}
	_, err = Plan([]Resource{r}, map[ID]Observed{r.ID(): owned(Instance, nil)})
	if err == nil {
		t.Fatal("ownership override accepted")
	}
}
func TestMissingAbsentIsNoop(t *testing.T) {
	got, err := Plan([]Resource{{Kind: Volume, Name: "missing", State: Absent}}, nil)
	if err != nil || got[0].Action != Noop {
		t.Fatalf("got %v %v", got, err)
	}
}
