package firewall

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEgressJournalAndLifecycleLock(t *testing.T) {
	dir := t.TempDir()
	unlock, err := LockEgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := LockEgress(dir); err == nil {
		second()
		t.Fatal("concurrent operation acquired lock")
	}
	unlock()
	unlock, err = LockEgress(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	path := EgressStateFile(filepath.Join(dir, "state.json"))
	if state, err := path.Load(); err != nil || state.Pending != nil {
		t.Fatalf("absent journal: %v", err)
	}
	grant := testEgressGrant(t)
	want := EgressState{Pending: &grant}
	if err := path.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := path.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("pending intent lost: %v", err)
	}
	info, err := os.Stat(string(path))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("journal is not private")
	}
	if err := os.WriteFile(string(path), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := path.Load(); err == nil {
		t.Fatal("accepted corrupt journal")
	}
}
