package orchestration

import (
	"strings"
	"testing"
)

const streamFirst = `apiVersion: aginctus.dev/v1alpha1
kind: Orchestration
metadata:
  name: aginctus
steps:
  - id: network
    driver: apply
    configuration:
      operation: upsert
`
const streamSecond = `{"apiVersion":"aginctus.dev/v1alpha1","kind":"Orchestration","metadata":{"name":"aginctus"},"steps":[{"id":"workload","driver":"apply","configuration":{"operation":"upsert"}}]}`

func TestParsePlanStreamMixedDocuments(t *testing.T) {
	p, err := ParsePlanStream([]byte(streamFirst + "---\n" + streamSecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 2 || p.Steps[0].ID != "network" || p.Steps[1].ID != "workload" {
		t.Fatalf("steps: %#v", p.Steps)
	}
}

func TestParsePlanStreamRejectsInvalidEntireInput(t *testing.T) {
	cases := map[string]string{
		"empty":                 "",
		"duplicate steps":       streamFirst + "---\n" + streamFirst,
		"bad second":            streamFirst + "---\nkind: Broken\n",
		"trailing junk":         streamFirst + "---\n[",
		"mismatched name":       streamFirst + "---\n" + strings.Replace(streamFirst, "name: aginctus", "name: other", 1),
		"duplicate YAML fields": strings.Replace(streamFirst, "kind: Orchestration", "kind: Orchestration\nkind: Orchestration", 1),
		"unknown field":         strings.Replace(streamFirst, "kind: Orchestration", "kind: Orchestration\nunsupported: true", 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePlanStream([]byte(input)); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestParsePlanStreamJSONRenderRoundTrip(t *testing.T) {
	input := []byte(streamSecond)
	p, err := ParsePlanStream(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 1 || p.Steps[0].ID != "workload" {
		t.Fatalf("roundtrip: %#v", p)
	}
}
