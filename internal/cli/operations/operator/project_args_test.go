package operatorops

import "testing"

// Project mutation and read-only preview parse explicit non-default scopes
// before any Incus capability is made available.
func TestOperatorProjectPlanArgs(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"--project"}, {"--project="}, {"--project=default"},
		{"--project", "default"}, {"--project", "bad space"},
		{"--project", "aginctus", "--force"}, {"--project", "../escape"},
	} {
		if _, err := ParseProjectArgs(args); err == nil {
			t.Errorf("unsafe project arguments accepted: %q", args)
		}
	}
	for _, args := range [][]string{
		{"--project=aginctus"}, {"--project", "aginctus"},
		{"--project", "aginctus-dev"},
	} {
		if _, err := ParseProjectArgs(args); err != nil {
			t.Errorf("valid project arguments rejected: %q: %v", args, err)
		}
	}
}

func TestOperatorProjectEnsureArgs(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"--project", "aginctus"}, {"--project=aginctus"},
		{"--project", "default", "--confirm-create"},
		{"--project=../outside", "--confirm-create"},
		{"--confirm-create", "--project=aginctus"},
		{"--project", "aginctus", "--confirm-create", "--force"},
	} {
		if _, err := ParseProjectEnsureArgs(args); err == nil {
			t.Errorf("accepted incomplete or unsafe project-ensure args: %q", args)
		}
	}
	for _, args := range [][]string{
		{"--project", "aginctus", "--confirm-create"},
		{"--project=aginctus", "--confirm-create"},
	} {
		scope, err := ParseProjectEnsureArgs(args)
		if err != nil || scope.Name != "aginctus" {
			t.Errorf("rejected valid explicit selection %q: scope=%v err=%v", args, scope, err)
		}
	}
}
