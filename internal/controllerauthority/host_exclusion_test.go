package controllerauthority

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createHostExclusionFixture(t *testing.T) (string, uint32, Enrollment, SelectedRevision) {
	t.Helper()
	dir := t.TempDir()
	owner := uint32(os.Geteuid())
	rec := testRecord()
	rec.ControllerID = "system-controller"
	rec.Domain = "host"
	rec.UID = 0
	rec.Certificate = ""
	rec.Epoch = 1
	if _, err := createAt(context.Background(), dir, owner, rec); err != nil {
		t.Fatal(err)
	}
	selected := SelectedRevision{
		DeploymentID: rec.DeploymentID, Project: rec.IncusProject,
		WorkloadID: rec.WorkloadID, RevisionID: rec.RevisionID,
		Generation: rec.Generation,
	}
	return dir, owner, rec, selected
}

func TestHostExclusionSerializesRootRevocationAndRechecksPolicy(t *testing.T) {
	dir, owner, rec, selected := createHostExclusionFixture(t)
	guard, err := acquireHostExclusionAt(context.Background(), dir, owner, selected, "start")
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := guard.Check(context.Background()); err != nil {
		t.Fatalf("live exclusion rejected: %v", err)
	}
	if second, err := acquireHostExclusionAt(context.Background(), dir, owner, selected, "start"); second != nil ||
		!errors.Is(err, ErrHostExclusionBusy) {
		t.Fatalf("two host controllers acquired one exclusion: %+v %v", second, err)
	}
	ref := EnrollmentReference{
		ControllerID: rec.ControllerID, DeploymentID: rec.DeploymentID,
		IncusProject: rec.IncusProject, WorkloadID: rec.WorkloadID,
	}
	if _, err := revokeAt(context.Background(), dir, owner, ref, 1); err == nil {
		t.Fatal("administrative revocation raced a held host exclusion")
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Check(context.Background()); !errors.Is(err, ErrHostExclusionInvalid) {
		t.Fatalf("closed exclusion remained valid: %v", err)
	}
	revoked, err := revokeAt(context.Background(), dir, owner, ref, 1)
	if err != nil || revoked.Status != "revoked" || revoked.Epoch != 2 {
		t.Fatalf("root administrative revocation not restored: %+v %v", revoked, err)
	}
	if _, err := acquireHostExclusionAt(context.Background(), dir, owner, selected, "start"); !errors.Is(err, ErrHostExclusionInvalid) {
		t.Fatalf("revoked controller reacquired exclusion: %v", err)
	}
}

func TestHostExclusionRejectsForeignOwnersAndStaleSelections(t *testing.T) {
	dir, owner, rec, selected := createHostExclusionFixture(t)
	for _, tc := range []struct{
		name string
		edit func(*SelectedRevision)
		action string
	}{
		{"foreign-project",func(s *SelectedRevision){s.Project="foreign"},"start"},
		{"unknown-workload",func(s *SelectedRevision){s.WorkloadID="foreign"},"start"},
		{"stale-revision",func(s *SelectedRevision){s.RevisionID=strings.Repeat("e",64)},"start"},
		{"stale-generation",func(s *SelectedRevision){s.Generation++},"start"},
		{"unknown-action",func(*SelectedRevision){},"delete"},
	} {
		t.Run(tc.name,func(t *testing.T){
			s := selected
			tc.edit(&s)
			if got,err := acquireHostExclusionAt(context.Background(),dir,owner,s,tc.action);got!=nil ||
				!errors.Is(err,ErrHostExclusionInvalid) {
				t.Fatalf("invalid host owner acquired exclusion: %+v %v",got,err)
			}
		})
	}
	// Change record to an operator. Root cannot impersonate its UID/TLS
	// identity merely by acquiring the root-controlled file lock.
	ref := EnrollmentReference{ControllerID:rec.ControllerID,DeploymentID:rec.DeploymentID,
		IncusProject:rec.IncusProject,WorkloadID:rec.WorkloadID}
	if _,err := revokeAt(context.Background(),dir,owner,ref,1);err!=nil {t.Fatal(err)}
	if _,err := acquireHostExclusionAt(context.Background(),dir,owner,selected,"start");err==nil {
		t.Fatal("revoked host controller accepted")
	}
}

func TestHostExclusionRejectsLockSubstitutionAndCancellation(t *testing.T) {
	dir, owner, _, selected := createHostExclusionFixture(t)
	ctx,cancel := context.WithCancel(context.Background())
	cancel()
	if guard,err := acquireHostExclusionAt(ctx,dir,owner,selected,"start");guard!=nil || !errors.Is(err,context.Canceled) {
		t.Fatalf("canceled exclusion was acquired: %+v %v",guard,err)
	}
	lockPath := filepath.Join(dir,hostEnrollmentLock)
	if err := os.Remove(lockPath);err!=nil{t.Fatal(err)}
	if err := os.Symlink(filepath.Join(t.TempDir(),"foreign"),lockPath);err!=nil{t.Fatal(err)}
	if guard,err := acquireHostExclusionAt(context.Background(),dir,owner,selected,"start");guard!=nil||err==nil{
		t.Fatalf("followed substituted lock inode: %+v %v",guard,err)
	}
}

func TestHostExclusionRootEntrypointCannotBeCalledByUser(t *testing.T) {
	if os.Geteuid() == 0 {t.Skip("root-only public API is exercised through test harness")}
	if guard,err := AcquireHostEnrollmentExclusion(context.Background(),SelectedRevision{},"start");guard!=nil || err==nil {
		t.Fatalf("nonroot acquired production host exclusion: %+v %v",guard,err)
	}
}
