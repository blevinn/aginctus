package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/blevinn/aginctus/internal/filestate"
)

const (
	localProjectStoreVersion = "aginctus.dev/local-project-state/v1alpha1"
	localProjectRecordLimit int64 = 1 << 20
)

// LocalProjectStateStore is a crash-durable, SAME-UID filesystem fixture of
// the ADR 0006 project/environments data layout. Its root must be explicitly
// provisioned on a trusted local filesystem; it is NOT automatically backed
// by an Incus project volume, authenticated, or safe to expose across users.
//
// The full authority is (verified Incus server, project); each environment
// UUID has its own revision pointer and independent cooperative CAS lock.
// No root/home-manager bridge, Incus daemon socket, privileged RPC, or live
// reconciliation is implemented here.
type LocalProjectStateStore struct {
	root string
	binding Binding
}

type localProjectRoot struct {
	APIVersion string  `json:"apiVersion"`
	Binding Binding    `json:"binding"`
}

type localEnvironmentRecord struct {
	Revision AcceptedRevision `json:"revision"`
	Candidate json.RawMessage `json:"candidate"`
}

func validateLocalProjectRoot(root string, binding Binding) error {
	if root == "/" || root == "" || !filepath.IsAbs(root) ||
		filepath.Clean(root) != root {
		return errors.New("local project state requires a normalized absolute root path")
	}
	if err := (Descriptor{
		ID: "7c82e6d1-1212-4123-8123-0123456789ab", Name: "project-state",
		Binding: binding,
	}).Validate(); err != nil {
		return errors.New("local project state requires a valid explicit Incus server and project")
	}
	return nil
}

// InitializeLocalProjectStateStore creates ONE new private filesystem fixture.
// It does not create/provision an Incus volume, enroll a caller, or determine
// the authenticated server/project. Trusted parent directories are required.
func InitializeLocalProjectStateStore(root string, binding Binding) (*LocalProjectStateStore, error) {
	if err := validateLocalProjectRoot(root, binding); err != nil {
		return nil, err
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		return nil, fmt.Errorf("create project state fixture: %w", err)
	}
	if err := os.Mkdir(filepath.Join(root, "environments"), 0o700); err != nil {
		return nil, err
	}
	info := localProjectRoot{APIVersion:localProjectStoreVersion,Binding:binding}
	data,err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	if err := createImmutableLocalRecord(root, "authority.json", data); err != nil {
		return nil, err
	}
	if err := syncLocalProjectDir(filepath.Dir(root)); err != nil {
		return nil, err
	}
	return OpenLocalProjectStateStore(root, binding)
}

// OpenLocalProjectStateStore only opens an existing fixture that was bound
// to the EXACT server/project identity at creation. Its binding argument is
// a selector, not an independently authenticated Incus connection.
func OpenLocalProjectStateStore(root string, binding Binding) (*LocalProjectStateStore, error) {
	if err := validateLocalProjectRoot(root, binding); err != nil {
		return nil, err
	}
	if err := checkPrivateLocalDir(root); err != nil { return nil,err }
	if err := checkPrivateLocalDir(filepath.Join(root, "environments")); err != nil { return nil,err }
	raw, err := filestate.ReadPrivateFile(filepath.Join(root,"authority.json"),localProjectRecordLimit)
	if err != nil { return nil,err }
	if err := rejectCandidateDuplicateKeys(raw); err != nil {
		return nil, errors.New("invalid local project authority record")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw,&fields) != nil || candidateKeys(fields,"apiVersion","binding") != nil {
		return nil, errors.New("invalid local project authority envelope")
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(fields["binding"],&keys) != nil || candidateKeys(keys,"serverId","project") != nil {
		return nil, errors.New("invalid local project authority binding")
	}
	var saved localProjectRoot
	if json.Unmarshal(raw,&saved) != nil || saved.APIVersion != localProjectStoreVersion ||
		saved.Binding != binding {
		return nil, errors.New("local project state binding mismatch")
	}
	return &LocalProjectStateStore{root:root,binding:binding},nil
}

func checkPrivateLocalDir(path string) error {
	info,err := os.Lstat(path)
	if err != nil {return err}
	stat,ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink!=0 ||
		info.Mode().Perm()&0o077!=0 || stat.Uid!=uint32(os.Geteuid()) {
		return errors.New("project state directory must be locally owned and private")
	}
	return nil
}

func syncLocalProjectDir(dir string) error {
	f,err := os.Open(dir)
	if err!=nil{return err}
	defer f.Close()
	return f.Sync()
}

func createImmutableLocalRecord(dir, name string, data []byte) error {
	if len(data)==0 || int64(len(data))>localProjectRecordLimit {
		return errors.New("invalid environment revision record size")
	}
	temp,err:=os.CreateTemp(dir,".pending-*")
	if err!=nil{return err}
	defer os.Remove(temp.Name())
	if err:=temp.Chmod(0o600);err!=nil{temp.Close();return err}
	if _,err:=temp.Write(data);err!=nil{temp.Close();return err}
	if err:=temp.Sync();err!=nil{temp.Close();return err}
	if err:=temp.Close();err!=nil{return err}
	target:=filepath.Join(dir,name)
	if err:=os.Link(temp.Name(),target);err!=nil{
		if !errors.Is(err,os.ErrExist){return err}
		existing,readErr:=filestate.ReadPrivateFile(target,localProjectRecordLimit)
		if readErr!=nil{return readErr}
		if !bytes.Equal(existing,data){return errors.New("accepted record collision or tampering")}
	}
	return syncLocalProjectDir(dir)
}

func publishLocalProjectPointer(dir string, data []byte) error {
	temp,err:=os.CreateTemp(dir,".active-*")
	if err!=nil{return err}
	defer os.Remove(temp.Name())
	if err:=temp.Chmod(0o600);err!=nil{temp.Close();return err}
	if _,err:=temp.Write(data);err!=nil{temp.Close();return err}
	if err:=temp.Sync();err!=nil{temp.Close();return err}
	if err:=temp.Close();err!=nil{return err}
	if err:=os.Rename(temp.Name(),filepath.Join(dir,"active.json"));err!=nil{return err}
	return syncLocalProjectDir(dir)
}

func (s *LocalProjectStateStore) namespace(desc Descriptor) (string,error) {
	if s==nil {return "",errors.New("missing local project state store")}
	if err:=desc.Validate();err!=nil{return "",err}
	if desc.Binding!=s.binding {return "",errors.New("environment binding differs from project state scope")}
	if err:=checkPrivateLocalDir(s.root);err!=nil{return "",err}
	if err:=checkPrivateLocalDir(filepath.Join(s.root,"environments"));err!=nil{return "",err}
	return filepath.Join(s.root,"environments",desc.ID),nil
}

func (s *LocalProjectStateStore) readNamespace(ctx context.Context, desc Descriptor) (*AcceptedRevision,*Candidate,error) {
	if err:=ctx.Err();err!=nil{return nil,nil,err}
	path,err:=s.namespace(desc)
	if err!=nil{return nil,nil,err}
	if err:=checkPrivateLocalDir(path);err!=nil{
		if errors.Is(err,os.ErrNotExist){return nil,nil,nil}
		return nil,nil,err
	}
	if err:=checkPrivateLocalDir(filepath.Join(path,"revisions"));err!=nil{return nil,nil,err}
	raw,err:=filestate.ReadPrivateFile(filepath.Join(path,"active.json"),localProjectRecordLimit)
	if err!=nil {
		if errors.Is(err,os.ErrNotExist){return nil,nil,nil}
		return nil,nil,err
	}
	if err:=rejectCandidateDuplicateKeys(raw);err!=nil{return nil,nil,errors.New("invalid local active pointer")}
	var pointerFields map[string]json.RawMessage
	if json.Unmarshal(raw,&pointerFields)!=nil ||
		candidateKeys(pointerFields,"apiVersion","environmentId","serverId","project","generation","parentId","candidateDigest","id")!=nil {
		return nil,nil,errors.New("invalid local active pointer fields")
	}
	var pointer AcceptedRevision
	if json.Unmarshal(raw,&pointer)!=nil || pointer.ValidateIdentity()!=nil {
		return nil,nil,errors.New("invalid local active pointer identity")
	}
	if pointer.EnvironmentID!=desc.ID || pointer.ServerID!=s.binding.ServerID ||
		pointer.Project!=s.binding.Project {return nil,nil,errors.New("foreign environment active pointer")}
	record, candidate, err:=s.readRevision(path,pointer.ID)
	if err!=nil{return nil,nil,err}
	if record!=pointer{return nil,nil,errors.New("active pointer differs from immutable revision")}
	if err:=ctx.Err();err!=nil{return nil,nil,err}
	return &pointer,&candidate,nil
}

func (s *LocalProjectStateStore) readRevision(path,id string)(AcceptedRevision,Candidate,error){
	if !validAcceptedHash(id){return AcceptedRevision{},Candidate{},errors.New("invalid accepted revision filename")}
	raw,err:=filestate.ReadPrivateFile(filepath.Join(path,"revisions",id+".json"),localProjectRecordLimit)
	if err!=nil{return AcceptedRevision{},Candidate{},err}
	if err:=rejectCandidateDuplicateKeys(raw);err!=nil{return AcceptedRevision{},Candidate{},errors.New("invalid immutable revision JSON")}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw,&fields)!=nil || candidateKeys(fields,"revision","candidate")!=nil {
		return AcceptedRevision{},Candidate{},errors.New("invalid immutable revision fields")
	}
	var rec localEnvironmentRecord
	if json.Unmarshal(raw,&rec)!=nil || rec.Revision.ID!=id {
		return AcceptedRevision{},Candidate{},errors.New("invalid immutable revision record")
	}
	candidate,err:=ParseCandidate(rec.Candidate)
	if err!=nil{return AcceptedRevision{},Candidate{},errors.New("invalid accepted candidate record")}
	if err:=rec.Revision.ValidateAgainst(candidate);err!=nil{return AcceptedRevision{},Candidate{},err}
	return rec.Revision,candidate,nil
}

// ReadLocalAccepted reads exactly ONE environment under a fixture's fixed
// project binding. An absent active pointer means no accepted revision in
// that local namespace, not proof of absent state on any Incus server.
// It never creates an environment, contacts Incus or performs a write.
func (s *LocalProjectStateStore) ReadLocalAccepted(ctx context.Context, enrolled Descriptor) (*AcceptedRevision,*Candidate,error) {
	if ctx==nil{return nil,nil,errors.New("local project read requires context")}
	return s.readNamespace(ctx,enrolled)
}

// CommitLocalCandidate is a SAME-UID fixture transaction, NOT authenticated
// project promotion. Caller-supplied enrolled/observed values are NOT proof of
// Incus permissions; do not wire this API directly to CLI/HTTP user requests.
// A real project service must own its authenticated Incus transport first.
func (s *LocalProjectStateStore) CommitLocalCandidate(
	ctx context.Context, enrolled Descriptor, observed Binding,
	expected *AcceptedRevision, candidate Candidate,
)(AcceptedRevision,error){
	if ctx==nil{return AcceptedRevision{},errors.New("local project commit requires context")}
	if err:=ctx.Err();err!=nil{return AcceptedRevision{},err}
	if _,err:=SelectCandidate(candidate,enrolled,observed);err!=nil{return AcceptedRevision{},err}
	path,err:=s.namespace(enrolled)
	if err!=nil{return AcceptedRevision{},err}
	if err:=os.Mkdir(path,0o700);err!=nil && !errors.Is(err,os.ErrExist) {return AcceptedRevision{},err}
	if err:=checkPrivateLocalDir(path);err!=nil{return AcceptedRevision{},err}
	revisions:=filepath.Join(path,"revisions")
	if err:=os.Mkdir(revisions,0o700);err!=nil && !errors.Is(err,os.ErrExist){return AcceptedRevision{},err}
	if err:=checkPrivateLocalDir(revisions);err!=nil{return AcceptedRevision{},err}
	lock,err:=filestate.LockRegistry(filepath.Join(path,"active.json"))
	if err!=nil{return AcceptedRevision{},fmt.Errorf("local environment CAS lock unavailable: %w",err)}
	defer lock.Close()
	current,prior,err:=s.readNamespace(ctx,enrolled)
	if err!=nil{return AcceptedRevision{},err}
	if (expected==nil)!=(current==nil) {return AcceptedRevision{},ErrAcceptedConflict}
	if expected!=nil && *expected!=*current {return AcceptedRevision{},ErrAcceptedConflict}
	next,err:=BuildAcceptedRevision(current,prior,candidate)
	if err!=nil{return AcceptedRevision{},err}
	body,err:=candidate.Canonical()
	if err!=nil{return AcceptedRevision{},err}
	immutable,err:=json.Marshal(localEnvironmentRecord{Revision:next,Candidate:body})
	if err!=nil{return AcceptedRevision{},err}
	if err:=ctx.Err();err!=nil{return AcceptedRevision{},err}
	if err:=createImmutableLocalRecord(revisions,next.ID+".json",immutable);err!=nil{return AcceptedRevision{},err}
	pointer,err:=json.Marshal(next)
	if err!=nil{return AcceptedRevision{},err}
	if err:=ctx.Err();err!=nil{return AcceptedRevision{},err}
	if err:=publishLocalProjectPointer(path,pointer);err!=nil {
		// Rename can have completed before a directory sync failure.
		return AcceptedRevision{},fmt.Errorf("local accepted pointer outcome uncertain; re-read before retry: %w",err)
	}
	return next,nil
}

// ReadLocalRevision locates an immutable historical revision by ID in the
// selected environment, never by display name or a project-global lookup.
// It rejects foreign revisions and broken content/identity relationships.
func (s *LocalProjectStateStore) ReadLocalRevision(ctx context.Context, enrolled Descriptor, id string)(AcceptedRevision,Candidate,error){
	if ctx==nil{return AcceptedRevision{},Candidate{},errors.New("local historical read requires context")}
	if err:=ctx.Err();err!=nil{return AcceptedRevision{},Candidate{},err}
	path,err:=s.namespace(enrolled)
	if err!=nil{return AcceptedRevision{},Candidate{},err}
	if err:=checkPrivateLocalDir(path);err!=nil{return AcceptedRevision{},Candidate{},err}
	if err:=checkPrivateLocalDir(filepath.Join(path,"revisions"));err!=nil{return AcceptedRevision{},Candidate{},err}
	rev,c,err:=s.readRevision(path,id)
	if err!=nil{return AcceptedRevision{},Candidate{},err}
	if rev.EnvironmentID!=enrolled.ID || rev.ServerID!=s.binding.ServerID || rev.Project!=s.binding.Project{
		return AcceptedRevision{},Candidate{},errors.New("historical revision belongs to different environment")
	}
	if err:=ctx.Err();err!=nil{return AcceptedRevision{},Candidate{},err}
	return rev,c,nil
}
