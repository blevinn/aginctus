package native

import "testing"

func TestSelectionValidation(t *testing.T) {
	valid := []Resource{{Kind: Network, Name: "aginctus-mgmt"}, {Kind: Instance, Project: "workloads", Name: "agent"}}
	if err := ValidateSelection(valid); err != nil {
		t.Fatal(err)
	}
	cases := [][]Resource{
		{{Kind: "bogus", Name: "x"}},
		{{Kind: Project, Project: "foo", Name: "bar"}},
		{{Kind: Network, Name: ""}},
		{{Kind: Volume, Name: "vol", State: Absent, Config: map[string]string{"x": "y"}}},
		{{Kind: Network, Name: "x"}, {Kind: Network, Name: "x"}},
	}
	for _, resources := range cases {
		if err := ValidateSelection(resources); err == nil {
			t.Errorf("expected rejection of %+v", resources)
		}
	}
}
