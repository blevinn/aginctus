package workloadops

import "testing"

// Ambiguous or foreign legacy Incus observations must not be read as owned.
func TestFindManagedInstance(t *testing.T) {
	cases := []struct {
		name      string
		instances []InspectedInstance
		want      string
		fail      bool
	}{
		{"absent", nil, "", false},
		{"owned", []InspectedInstance{{Name: "aginctus-dev", Status: "Running", Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "dev"}}}, "Running", false},
		{"foreign", []InspectedInstance{{Name: "aginctus-dev", Status: "Running"}}, "", true},
		{"wrong id", []InspectedInstance{{Name: "aginctus-dev", Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "other"}}}, "", true},
		{"wrong project", []InspectedInstance{{Name: "aginctus-dev", Project: "guest", Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "dev"}}}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FindManagedInstance(tc.instances, "aginctus-dev", "workload", "agent-workload", "dev")
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("status=%q error=%v", got, err)
			}
		})
	}
}
