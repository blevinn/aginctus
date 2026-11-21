package environment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"

	"github.com/blevinn/aginctus/internal/filestate"
)

const localEnvironmentAttemptVersion = "aginctus.dev/local-environment-attempt/v1alpha1"

var localEnvironmentAttemptID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// LocalEnvironmentPin keeps the SAME per-environment advisory lock used by
// CommitLocalCandidate. It serializes participating, same-UID fixture actors
// only. It is not a project-authenticated Incus writer fence or a capability.
type LocalEnvironmentPin struct {
	mu sync.Mutex
	store *LocalProjectStateStore
	lock *os.File
	enrolled Descriptor
	revision AcceptedRevision
	workloadID string
	closed bool
}

// PinnedEnvironmentWorkload is nonsecret accepted intent copied from the
// immutable local fixture. It does not carry TLS, Incus ETag or write authority.
type PinnedEnvironmentWorkload struct {
	Revision AcceptedRevision `json:"revision"`
	Environment Descriptor `json:"environment"`
	WorkloadID string `json:"workloadId"`
	WorkloadIndex int `json:"workloadIndex"`
}

// PinLocalAccepted pins ONE exact accepted revision and one selected workload,
// never a candidate file or ambient CLI project. Its caller must separately
// prove the Incus project authority before any real reconciliation.
func (s *LocalProjectStateStore) PinLocalAccepted(
	ctx context.Context, enrolled Descriptor, expected AcceptedRevision, workloadID string,
)(*LocalEnvironmentPin,error){
	if ctx==nil{return nil,errors.New("local environment pin requires context")}
	if err:=ctx.Err();err!=nil{return nil,err}
	if !idPattern.MatchString(enrolled.ID) || expected.ValidateIdentity()!=nil ||
		expected.EnvironmentID!=enrolled.ID || expected.ServerID!=enrolled.Binding.ServerID ||
		expected.Project!=enrolled.Binding.Project || workloadID=="" {
		return nil,ErrAcceptedConflict
	}
	path,err:=s.namespace(enrolled)
	if err!=nil{return nil,err}
	if err:=checkPrivateLocalDir(path);err!=nil{return nil,err}
	if err:=checkPrivateLocalDir(filepath.Join(path,"revisions"));err!=nil{return nil,err}
	lock,err:=filestate.LockRegistry(filepath.Join(path,"active.json"))
	if err!=nil{return nil,fmt.Errorf("lock local environment accepted pointer: %w",err)}
	p:=&LocalEnvironmentPin{
		store:s,lock:lock,enrolled:enrolled,revision:expected,workloadID:workloadID,
	}
	if err:=p.check(ctx);err!=nil{
		_ = lock.Close()
		return nil,err
	}
	return p,nil
}

func (p *LocalEnvironmentPin) check(ctx context.Context) error {
	if ctx==nil{return errors.New("local accepted pin requires context")}
	if err:=ctx.Err();err!=nil{return err}
	actual,candidate,err:=p.store.ReadLocalAccepted(ctx,p.enrolled)
	if err!=nil{return err}
	if actual==nil || candidate==nil || *actual!=p.revision ||
		!slices.Contains(candidate.SelectedWorkloads,p.workloadID){
		return ErrAcceptedConflict
	}
	return ctx.Err()
}

// Check re-reads the active pointer while the fixture exclusion lock is held.
// Even success does not authorize Incus writes, since Incus project users and
// external/manual writers do not participate in this lock.
func (p *LocalEnvironmentPin) Check(ctx context.Context)error{
	if p==nil{return ErrAcceptedConflict}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed{return ErrAcceptedConflict}
	return p.check(ctx)
}

// SnapshotCandidate returns a deep, independently parsed copy of the pinned
// accepted candidate. Its caller MUST keep this pin held through its use.
func (p *LocalEnvironmentPin) SnapshotCandidate(ctx context.Context)(Candidate,error){
	if p==nil{return Candidate{},ErrAcceptedConflict}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed{return Candidate{},ErrAcceptedConflict}
	if err:=p.check(ctx);err!=nil{return Candidate{},err}
	_,candidate,err:=p.store.ReadLocalAccepted(ctx,p.enrolled)
	if err!=nil{return Candidate{},err}
	if candidate==nil{return Candidate{},ErrAcceptedConflict}
	data,err:=candidate.Canonical()
	if err!=nil{return Candidate{},err}
	return ParseCandidate(data)
}

func (p *LocalEnvironmentPin) Close()error{
	if p==nil{return nil}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed{return nil}
	p.closed=true
	return p.lock.Close()
}

// LocalUnverifiedEnvironmentAttempt is an immutable, nonsecret historical
// operation intention; "unverified" is its ONLY outcome. It is never a
// success marker, fence or permission to invoke the native Incus SDK.
type LocalUnverifiedEnvironmentAttempt struct {
	APIVersion string `json:"apiVersion"`
	ID string `json:"id"`
	EnvironmentID string `json:"environmentId"`
	ServerID string `json:"serverId"`
	Project string `json:"project"`
	RevisionID string `json:"revisionId"`
	Generation uint64 `json:"generation"`
	WorkloadID string `json:"workloadId"`
	Operation string `json:"operation"`
	Outcome string `json:"outcome"`
}

func (a LocalUnverifiedEnvironmentAttempt) validate()error{
	if a.APIVersion!=localEnvironmentAttemptVersion || !localEnvironmentAttemptID.MatchString(a.ID) ||
		!idPattern.MatchString(a.EnvironmentID) || a.ServerID=="" || a.Project=="" ||
		!validAcceptedHash(a.RevisionID) || a.Generation==0 ||
		a.WorkloadID=="" || a.Operation!="start" || a.Outcome!="unverified"{
		return errors.New("invalid or falsely completed local environment operation")
	}
	return nil
}

// RecordLocalUnverifiedStart durably appends a bounded immutable intent while
// the exact accepted revision pin remains held. The operation does not start
// an instance, claim a completed outcome, or carry an executable capability.
// No implicit stop/delete is recorded for ensureRunning=false.
func (p *LocalEnvironmentPin) RecordLocalUnverifiedStart(ctx context.Context)(LocalUnverifiedEnvironmentAttempt,error){
	if p==nil{return LocalUnverifiedEnvironmentAttempt{},ErrAcceptedConflict}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed{return LocalUnverifiedEnvironmentAttempt{},ErrAcceptedConflict}
	if err:=p.check(ctx);err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	_,candidate,err:=p.store.ReadLocalAccepted(ctx,p.enrolled)
	if err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	found:=false
	for _,w:=range candidate.Workloads{
		if w.ID==p.workloadID{
			found=w.EnsureRunning
			break
		}
	}
	if !found{return LocalUnverifiedEnvironmentAttempt{},errors.New("accepted workload has no affirmative start intent")}
	nonce:=make([]byte,16)
	if _,err:=rand.Read(nonce);err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	record:=LocalUnverifiedEnvironmentAttempt{
		APIVersion:localEnvironmentAttemptVersion,ID:hex.EncodeToString(nonce),
		EnvironmentID:p.enrolled.ID,ServerID:p.enrolled.Binding.ServerID,
		Project:p.enrolled.Binding.Project,RevisionID:p.revision.ID,
		Generation:p.revision.Generation,WorkloadID:p.workloadID,
		Operation:"start",Outcome:"unverified",
	}
	if err:=record.validate();err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	path,err:=p.store.namespace(p.enrolled)
	if err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	operations:=filepath.Join(path,"operations")
	if err:=os.Mkdir(operations,0o700);err!=nil && !errors.Is(err,os.ErrExist) {
		return LocalUnverifiedEnvironmentAttempt{},err
	}
	if err:=checkPrivateLocalDir(operations);err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if err:=syncLocalProjectDir(path);err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	raw,err:=json.Marshal(record)
	if err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if err:=ctx.Err();err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if err:=createImmutableLocalRecord(operations,record.ID+".json",raw);err!=nil{
		return LocalUnverifiedEnvironmentAttempt{},fmt.Errorf("local operation outcome uncertain; re-read journal: %w",err)
	}
	return record,nil
}

// ReadLocalUnverifiedStart recovers one previously published immutable attempt
// by explicit environment and ID, including after the process holding its pin
// has exited. It verifies the referenced historical revision and workload;
// later acceptance never silently marks an old attempt successful.
func (s *LocalProjectStateStore) ReadLocalUnverifiedStart(ctx context.Context,enrolled Descriptor,id string)(LocalUnverifiedEnvironmentAttempt,error){
	if ctx==nil{return LocalUnverifiedEnvironmentAttempt{},errors.New("operation lookup requires context")}
	if err:=ctx.Err();err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if !localEnvironmentAttemptID.MatchString(id){return LocalUnverifiedEnvironmentAttempt{},errors.New("invalid operation ID")}
	path,err:=s.namespace(enrolled)
	if err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if err:=checkPrivateLocalDir(path);err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if err:=checkPrivateLocalDir(filepath.Join(path,"operations"));err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	raw,err:=filestate.ReadPrivateFile(filepath.Join(path,"operations",id+".json"),localProjectRecordLimit)
	if err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if err:=rejectCandidateDuplicateKeys(raw);err!=nil{return LocalUnverifiedEnvironmentAttempt{},errors.New("invalid immutable operation JSON")}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw,&fields)!=nil || candidateKeys(fields,
		"apiVersion","id","environmentId","serverId","project","revisionId",
		"generation","workloadId","operation","outcome")!=nil{
		return LocalUnverifiedEnvironmentAttempt{},errors.New("invalid immutable operation fields")
	}
	var record LocalUnverifiedEnvironmentAttempt
	if json.Unmarshal(raw,&record)!=nil || record.validate()!=nil || record.ID!=id ||
		record.EnvironmentID!=enrolled.ID || record.ServerID!=enrolled.Binding.ServerID ||
		record.Project!=enrolled.Binding.Project {
		return LocalUnverifiedEnvironmentAttempt{},errors.New("foreign or invalid local operation record")
	}
	if err:=checkPrivateLocalDir(filepath.Join(path,"revisions"));err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	revision,candidate,err:=s.readRevision(path,record.RevisionID)
	if err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	if revision.EnvironmentID!=enrolled.ID || revision.ServerID!=enrolled.Binding.ServerID ||
		revision.Project!=enrolled.Binding.Project || revision.Generation!=record.Generation ||
		!slices.Contains(candidate.SelectedWorkloads,record.WorkloadID) {
		return LocalUnverifiedEnvironmentAttempt{},errors.New("operation does not match accepted history")
	}
	validStart:=false
	for _,w:=range candidate.Workloads{
		if w.ID==record.WorkloadID && w.EnsureRunning {validStart=true;break}
	}
	if !validStart{return LocalUnverifiedEnvironmentAttempt{},errors.New("operation has no accepted start intent")}
	if err:=ctx.Err();err!=nil{return LocalUnverifiedEnvironmentAttempt{},err}
	return record,nil
}
