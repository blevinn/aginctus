package acceptedstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/blevinn/aginctus/internal/filestate"
)

const maxRecordBytes int64 = 1 << 20

// LocalStore is a narrowly scoped, SAME-UID, local proof of durable revision
// acceptance. It is NOT the authenticated state-service API or an Incus
// bootstrap mechanism and MUST NOT be exposed as a user-to-root helper.
// The caller must supply a trusted absolute parent path.
type LocalStore struct {
	root string
}

var ErrRevisionConflict = errors.New("accepted revision compare-and-swap conflict")

// InitializeLocalStore explicitly creates a private, empty local store.
// No Incus resources, credentials, or user-scope state are touched.
func InitializeLocalStore(root string) (*LocalStore, error) {
	if !filepath.IsAbs(root) || root == "/" {
		return nil, errors.New("store path must be absolute and private")
	}
	if err := os.Mkdir(root, 0o700); err != nil { return nil, fmt.Errorf("initialize local store: %w", err) }
	if err := os.Mkdir(filepath.Join(root, "revisions"), 0o700); err != nil {
		return nil, fmt.Errorf("create immutable revision directory: %w", err)
	}
	if err := syncDirectory(root); err != nil { return nil, err }
	if err := syncDirectory(filepath.Dir(root)); err != nil { return nil, err }
	return OpenLocalStore(root)
}

func OpenLocalStore(root string) (*LocalStore, error) {
	if !filepath.IsAbs(root) || root == "/" {
		return nil, errors.New("store path must be absolute and private")
	}
	if err := privateStoreDirectory(root); err != nil { return nil, err }
	if err := privateStoreDirectory(filepath.Join(root, "revisions")); err != nil { return nil, err }
	return &LocalStore{root: root}, nil
}

func privateStoreDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil { return fmt.Errorf("inspect local store directory: %w", err) }
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 || st.Uid != uint32(os.Geteuid()) {
		return errors.New("local state directory must be owned and private")
	}
	return nil
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	return f.Sync()
}

type storedRevision struct {
	Revision Revision        `json:"revision"`
	Spec     json.RawMessage `json:"spec"`
}

// ReadActive never trusts a pointer alone: it loads the named immutable
// record and verifies its digest and revision chain identity.
func (s *LocalStore) ReadActive() (*Revision, *DeploymentSpec, error) {
	if s == nil { return nil,nil,errors.New("nil local store") }
	if err := privateStoreDirectory(s.root); err != nil { return nil,nil,err }
	raw, err := filestate.ReadPrivateFile(filepath.Join(s.root,"active.json"),maxRecordBytes)
	if errors.Is(err,os.ErrNotExist) { return nil,nil,nil }
	if err != nil { return nil,nil,fmt.Errorf("read active pointer: %w",err) }
	var active Revision
	if err := json.Unmarshal(raw,&active); err != nil {
		return nil,nil,errors.New("invalid active revision pointer")
	}
	if len(active.ID)!=64 || !fingerprintHex(active.ID) {
		return nil,nil,errors.New("invalid active revision ID")
	}
	immutable, err := filestate.ReadPrivateFile(filepath.Join(s.root,"revisions",active.ID+".json"),maxRecordBytes)
	if err != nil { return nil,nil,fmt.Errorf("read immutable accepted revision: %w",err) }
	var record storedRevision
	if err := json.Unmarshal(immutable,&record); err != nil || record.Revision!=active {
		return nil,nil,errors.New("inconsistent immutable accepted revision")
	}
	spec,err := ParseDeploymentSpec(record.Spec)
	if err != nil { return nil,nil,errors.New("invalid immutable deployment spec") }
	if err := active.validateDigest(spec); err != nil { return nil,nil,err }
	return &active,&spec,nil
}

// CommitLocal performs single-principal CAS over one immutable workload-only
// revision. Expected generation + ID are checked UNDER an advisory lock.
// It does not authorize system/user scope, network transport or Incus writes.
func (s *LocalStore) CommitLocal(expectedGeneration uint64,expectedID string,spec DeploymentSpec) (Revision,error) {
	if s==nil { return Revision{},errors.New("nil local store") }
	if err := privateStoreDirectory(s.root); err != nil { return Revision{},err }
	if err := privateStoreDirectory(filepath.Join(s.root,"revisions"));err!=nil{return Revision{},err}
	lock,err:=filestate.LockRegistry(filepath.Join(s.root,"active.json"))
	if err!=nil {return Revision{},fmt.Errorf("lock local accepted revision: %w",err)}
	defer lock.Close()

	current,currentSpec,err:=s.ReadActive()
	if err!=nil {return Revision{},err}
	if current==nil {
		if expectedGeneration!=0 || expectedID!="" {return Revision{},ErrRevisionConflict}
	} else {
		if current.Generation!=expectedGeneration || current.ID!=expectedID {
			return Revision{},ErrRevisionConflict
		}
		if _,err:=CompareDeployments(*currentSpec,spec);err!=nil {return Revision{},err}
	}
	next,err:=BuildRevision(current,spec)
	if err!=nil{return Revision{},err}
	canonical,err:=spec.Canonical()
	if err!=nil{return Revision{},err}
	record,err:=json.Marshal(storedRevision{Revision:next,Spec:canonical})
	if err!=nil{return Revision{},err}

	// Content-addressed append before publishing the active pointer. A crash
	// here leaves an unreferenced record, never a partially active revision.
	if err := writeImmutableRecord(filepath.Join(s.root,"revisions"),next.ID+".json",record);err!=nil {
		return Revision{},err
	}
	pointer,err:=json.Marshal(next)
	if err!=nil{return Revision{},err}
	if err:=atomicPrivatePointer(s.root,"active.json",pointer);err!=nil {
		// Atomic replacement may have succeeded before sync failed: report
		// uncertainty, and require caller to re-read the authoritative pointer.
		return Revision{},fmt.Errorf("active pointer outcome uncertain; inspect state before retry: %w",err)
	}
	return next,nil
}

func writeImmutableRecord(dir,name string,data []byte) error {
	tmp,err:=os.CreateTemp(dir,".pending-*")
	if err!=nil {return err}
	defer os.Remove(tmp.Name())
	if err:=tmp.Chmod(0o600);err!=nil{tmp.Close();return err}
	if _,err:=tmp.Write(data);err!=nil{tmp.Close();return err}
	if err:=tmp.Sync();err!=nil{tmp.Close();return err}
	if err:=tmp.Close();err!=nil{return err}
	dest:=filepath.Join(dir,name)
	if err:=os.Link(tmp.Name(),dest);err!=nil {
		if !errors.Is(err,os.ErrExist){return err}
		existing,e:=filestate.ReadPrivateFile(dest,maxRecordBytes)
		if e!=nil{return e}
		if !bytes.Equal(existing,data){return errors.New("immutable revision ID collision or altered record")}
	}
	return syncDirectory(dir)
}

func atomicPrivatePointer(dir,name string,data []byte) error {
	tmp,err:=os.CreateTemp(dir,".active-*")
	if err!=nil {return err}
	defer os.Remove(tmp.Name())
	if err:=tmp.Chmod(0o600);err!=nil{tmp.Close();return err}
	if _,err:=tmp.Write(data);err!=nil{tmp.Close();return err}
	if err:=tmp.Sync();err!=nil{tmp.Close();return err}
	if err:=tmp.Close();err!=nil{return err}
	if err:=os.Rename(tmp.Name(),filepath.Join(dir,name));err!=nil{return err}
	return syncDirectory(dir)
}
