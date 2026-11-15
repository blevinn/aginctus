package controllerauthority

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRecord() Enrollment {
	return Enrollment{
		ControllerID: "hm-operator", Domain: "operator", UID: 1000,
		Certificate: strings.Repeat("a", 64), DeploymentID: "demo",
		IncusProject: "aginctus", WorkloadID: "agent",
		RevisionID: strings.Repeat("b", 64), Generation: 2,
		Epoch: 3, Actions: []string{"start", "stop"}, Status: "active",
	}
}
func encode(t *testing.T, entries ...Enrollment) []byte {
	t.Helper()
	b, err := json.Marshal(enrollmentFile{APIVersion: EnrollmentVersion, Enrollments: entries})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestEnrollmentStrictScopeAndDuplicateOwners(t *testing.T) {
	valid := testRecord()
	if err := ParseEnrollmentFile(encode(t, valid)); err != nil {
		t.Fatal(err)
	}
	duplicate := valid
	duplicate.ControllerID = "nixos-root"
	duplicate.Domain = "host"
	duplicate.UID = 0
	duplicate.Certificate = ""
	if err := ParseEnrollmentFile(encode(t, valid, duplicate)); err == nil {
		t.Fatal("allowed both system and user owner for the same resource")
	}
	for _, tc := range []struct{
		name string
		edit func(*Enrollment)
	}{
		{"default",func(e *Enrollment){e.IncusProject="default"}},
		{"cross-project",func(e *Enrollment){e.IncusProject="../foreign"}},
		{"root-hm",func(e *Enrollment){e.UID=0}},
		{"missing-restricted-tls",func(e *Enrollment){e.Certificate=""}},
		{"host-cert",func(e *Enrollment){e.Domain="host";e.UID=0}},
		{"unsupported-mode",func(e *Enrollment){e.Domain="shared"}},
		{"mutable-revision",func(e *Enrollment){e.RevisionID="latest"}},
		{"zero-generation",func(e *Enrollment){e.Generation=0}},
		{"no-epoch",func(e *Enrollment){e.Epoch=0}},
		{"no-action",func(e *Enrollment){e.Actions=nil}},
		{"ambient-action",func(e *Enrollment){e.Actions=[]string{"apply"}}},
		{"duplicate-action",func(e *Enrollment){e.Actions=[]string{"start","start"}}},
		{"unknown-status",func(e *Enrollment){e.Status="adopted"}},
	} {
		t.Run(tc.name,func(t *testing.T){
			e:=valid;tc.edit(&e)
			if err:=ParseEnrollmentFile(encode(t,e));err==nil {t.Fatal("unsafe enrollment accepted")}
		})
	}
}
func TestEnrollmentStrictJSONRejectsAliasingNullAndBroadFields(t *testing.T) {
	valid:=string(encode(t,testRecord()))
	for _,bad:= range []string{
		valid+"{}",
		"null",
		strings.Replace(valid,`"controllerID":`,`"controllerID":"other","controllerID":`,1),
		strings.Replace(valid,`"status":`,`"Status":"active","status":`,1),
		strings.Replace(valid,`"status":"active"`,`"status":null`,1),
		strings.Replace(valid,`"actions":[`,`"actions":null,"other":[`,1),
		strings.Replace(valid,`"uid":1000`,`"uid":null`,1),
		strings.Replace(valid,`"generation":2`,`"generation":2,"hostConfig":{"firewall":"off"}`,1),
		strings.Replace(valid,`"apiVersion":`,`"ApiVersion":"untrusted","apiVersion":`,1),
		strings.Replace(valid,`"enrollments":`,`"enrollments":null,"extra":`,1),
	} {
		if err:=ParseEnrollmentFile([]byte(bad));err==nil {t.Fatalf("unsafe schema accepted: %s",bad)}
	}
}
func TestOwnerAnchoredEvidenceAndRevocation(t *testing.T) {
	dir:=t.TempDir()
	write:=func(rec Enrollment){
		t.Helper()
		if err:=os.WriteFile(filepath.Join(dir,hostEnrollmentFile),encode(t,rec),0o600);err!=nil {t.Fatal(err)}
	}
	rec:=testRecord()
	write(rec)
	h,err:=loadOwnerFile(dir,uint32(os.Geteuid()))
	if err!=nil {t.Fatal(err)}
	peer:=AssessedPrincipal{UID:1000,TLSFingerprint:rec.Certificate,ProjectRestricted:true}
	selector:=SelectedRevision{DeploymentID:rec.DeploymentID,Project:rec.IncusProject,WorkloadID:rec.WorkloadID,RevisionID:rec.RevisionID,Generation:rec.Generation}
	if a:=h.Assess(peer,selector,"start");a.Code!=EnrollmentMatched || a.Epoch!=3 {
		t.Fatalf("expected candidate match, got %+v",a)
	}
	for _,tc:=range []struct{
		name string
		principal AssessedPrincipal
		selection SelectedRevision
		action string
	}{
		{"wrong-user",AssessedPrincipal{UID:1001,TLSFingerprint:rec.Certificate,ProjectRestricted:true},selector,"start"},
		{"not-restricted",AssessedPrincipal{UID:1000,TLSFingerprint:rec.Certificate},selector,"start"},
		{"wrong-cert",AssessedPrincipal{UID:1000,TLSFingerprint:strings.Repeat("f",64),ProjectRestricted:true},selector,"start"},
		{"wrong-project",peer,SelectedRevision{DeploymentID:"demo",Project:"foreign",WorkloadID:"agent",RevisionID:rec.RevisionID,Generation:2},"start"},
		{"stale",peer,SelectedRevision{DeploymentID:"demo",Project:"aginctus",WorkloadID:"agent",RevisionID:strings.Repeat("c",64),Generation:2},"start"},
		{"unselected",peer,SelectedRevision{DeploymentID:"demo",Project:"aginctus",WorkloadID:"other",RevisionID:rec.RevisionID,Generation:2},"start"},
		{"unknown-action",peer,selector,"replace"},
	} {
		t.Run(tc.name,func(t *testing.T){
			if a:=h.Assess(tc.principal,tc.selection,tc.action);a.Code==EnrollmentMatched {
				t.Fatal("mismatched principal or selection obtained an eligible assessment")
			}
		})
	}
	rec.Status="revoked"
	write(rec)
	reloaded,err:=loadOwnerFile(dir,uint32(os.Geteuid()))
	if err!=nil {t.Fatal(err)}
	if a:=reloaded.Assess(peer,selector,"start");a.Code!=EnrollmentRevoked {t.Fatalf("revoked entry matched: %+v",a)}
	// The old in-memory snapshot is not a revocation oracle. The real
	// mutation boundary MUST load fresh host policy (and independent fence).
	if a:=h.Assess(peer,selector,"start");a.Code!=EnrollmentMatched {t.Fatal("unexpected cached evidence state")}
}
func TestEnrollmentRejectsSymlinksWritableOwnerDriftAndDirectoryReplacement(t *testing.T) {
	dir:=t.TempDir()
	file:=filepath.Join(dir,hostEnrollmentFile)
	if err:=os.WriteFile(file,encode(t,testRecord()),0o600);err!=nil {t.Fatal(err)}
	owner:=uint32(os.Geteuid())
	if err:=os.Chmod(file,0o666);err!=nil {t.Fatal(err)}
	if _,err:=loadOwnerFile(dir,owner);err==nil {t.Fatal("accepted group-writable enrollment")}
	if err:=os.Chmod(file,0o600);err!=nil {t.Fatal(err)}
	if err:=os.Chmod(dir,0o777);err!=nil {t.Fatal(err)}
	if _,err:=loadOwnerFile(dir,owner);err==nil {t.Fatal("accepted group-writable enrollment directory")}
	if err:=os.Chmod(dir,0o700);err!=nil {t.Fatal(err)}
	if err:=os.Remove(file);err!=nil {t.Fatal(err)}
	if err:=os.Symlink(filepath.Join(t.TempDir(),"absent"),file);err!=nil {t.Fatal(err)}
	if _,err:=loadOwnerFile(dir,owner);err==nil {t.Fatal("followed symlink")}
	if err:=os.Remove(file);err!=nil {t.Fatal(err)}
	if err:=os.WriteFile(file,encode(t,testRecord()),0o600);err!=nil {t.Fatal(err)}
	linkDir:=filepath.Join(t.TempDir(),"link")
	if err:=os.Symlink(dir,linkDir);err!=nil {t.Fatal(err)}
	if _,err:=loadOwnerFile(linkDir,owner);err==nil {t.Fatal("followed symlink directory")}
}
