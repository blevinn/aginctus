package environment

import "testing"

func sample() Descriptor {
	return Descriptor{ID: "7c82e6d1-1212-4123-8123-0123456789ab", Name: "development", Binding: Binding{ServerID: "verified-server-fingerprint", Project: "personal-dev"}}
}

func TestIdentityAndBinding(t *testing.T) {
	d := sample()
	if err := d.Validate(); err != nil { t.Fatal(err) }
	if err := d.VerifyBinding(d.Binding); err != nil { t.Fatal(err) }
	renamed := d
	renamed.Name = "renamed"
	if !d.SameEnvironment(renamed) { t.Fatal("rename changed environment identity") }
	for _, other := range []Binding{{ServerID: "different", Project: d.Binding.Project},{ServerID:d.Binding.ServerID, Project:"other"}} {
		if err := d.VerifyBinding(other); err == nil { t.Fatalf("accepted changed binding: %+v", other) }
	}
}

func TestInvalidDescriptors(t *testing.T) {
	for _, mutate := range []func(*Descriptor){
		func(d *Descriptor){ d.ID = "development" },
		func(d *Descriptor){ d.ID = "7C82E6D1-1212-4123-8123-0123456789AB" },
		func(d *Descriptor){ d.Name = "../escaped" },
		func(d *Descriptor){ d.Binding.ServerID = "" },
		func(d *Descriptor){ d.Binding.ServerID = "alias\nother" },
		func(d *Descriptor){ d.Binding.Project = "" },
		func(d *Descriptor){ d.Binding.Project = "../default" },
	} {
		d := sample()
		mutate(&d)
		if err := d.Validate(); err == nil { t.Fatalf("accepted invalid descriptor: %+v", d) }
	}
}

func TestMultipleEnvironmentsInProject(t *testing.T) {
	a := sample()
	b := sample()
	b.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
	b.Name = "staging"
	if err := b.Validate(); err != nil { t.Fatal(err) }
	if a.SameEnvironment(b) { t.Fatal("environments in same project collapsed together") }
}

func TestBindingProjectMatchesIncusProjectScope(t *testing.T) {
	for _, project := range []string{"dev_team", "Team.1", "A-2", "a"} {
		d := sample()
		d.Binding.Project = project
		if err := d.Validate(); err != nil {
			t.Fatalf("Incus-accepted project %q rejected: %v", project, err)
		}
	}
	for _, project := range []string{"default", "", "../escape", "bad/name", " bad", "bad\nname"} {
		d := sample()
		d.Binding.Project = project
		if err := d.Validate(); err == nil {
			t.Fatalf("accepted invalid or unsupported Incus project %q", project)
		}
	}
}
