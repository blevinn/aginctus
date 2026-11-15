package controllerauthority

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readOnlyPeerFixture(t *testing.T) (string, string, uint32, Enrollment, *PeerEnrollmentService) {
	t.Helper()
	policyDir := filepath.Join(t.TempDir(), "authority")
	if err := os.Mkdir(policyDir, 0o700); err != nil { t.Fatal(err) }
	owner := uint32(os.Geteuid())
	rec := testRecord()
	rec.Epoch = 1
	if owner == 0 {
		rec.Domain = "host"
		rec.UID = 0
		rec.Certificate = ""
	} else {
		rec.UID = owner
	}
	if _, err := createAt(context.Background(), policyDir, owner, rec); err != nil { t.Fatal(err) }
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil { t.Fatal(err) }
	socket := filepath.Join(runtimeDir, "peer.sock")
	svc, err := startPeerEnrollmentAt(context.Background(), policyDir, socket, owner)
	if err != nil { t.Fatal(err) }
	t.Cleanup(func(){ _ = svc.Close() })
	return policyDir, socket, owner, rec, svc
}

func peerReadOnlyClient(socket string) *http.Client {
	return &http.Client{
		Timeout: 5*time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
			DisableKeepAlives: true,
		},
	}
}

func peerReadOnlyURL(rec Enrollment) string {
	q := url.Values{}
	q.Set("deployment", rec.DeploymentID)
	q.Set("project", rec.IncusProject)
	q.Set("workload", rec.WorkloadID)
	q.Set("revision", rec.RevisionID)
	q.Set("generation", strconv.FormatUint(rec.Generation,10))
	q.Set("action","start")
	return "http://localhost/v1/peer-enrollment?"+q.Encode()
}

func TestPeerEnrollmentServiceReadsKernelPeerAndLiveHostPolicy(t *testing.T) {
	dir,socket,owner,rec,_ := readOnlyPeerFixture(t)
	client := peerReadOnlyClient(socket)
	response,err := client.Get(peerReadOnlyURL(rec))
	if err != nil { t.Fatal(err) }
	defer response.Body.Close()
	var report struct { Code string `json:"code"` }
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {t.Fatal(err)}
	if response.StatusCode!=http.StatusOK || report.Code!="peer-uid-enrolled-not-authorized" {
		t.Fatalf("peer enrollment reported incorrect authority: %d %+v",response.StatusCode,report)
	}
	ref := EnrollmentReference{ControllerID:rec.ControllerID,
		DeploymentID:rec.DeploymentID,IncusProject:rec.IncusProject,WorkloadID:rec.WorkloadID}
	revoked,err := revokeAt(context.Background(),dir,owner,ref,1)
	if err != nil || revoked.Status!="revoked" {t.Fatalf("host revocation failed: %+v %v",revoked,err)}
	second,err := client.Get(peerReadOnlyURL(rec))
	if err != nil {t.Fatal(err)}
	second.Body.Close()
	if second.StatusCode!=http.StatusForbidden {
		t.Fatalf("revoked live policy still admitted peer UID: %d",second.StatusCode)
	}
}

func TestPeerEnrollmentServiceRefusesForgedSelectorsAndWriteMethods(t *testing.T) {
	_,socket,owner,rec,_ := readOnlyPeerFixture(t)
	client := peerReadOnlyClient(socket)
	base := peerReadOnlyURL(rec)
	cases := []struct {name,path string; status int}{
		{"self-reported-root-uid",base+"&uid=0",http.StatusBadRequest},
		{"self-reported-tls",base+"&certificateFingerprint="+strings.Repeat("a",64),http.StatusBadRequest},
		{"foreign-project",strings.Replace(base,"project=aginctus","project=foreign",1),http.StatusForbidden},
		{"stale-revision",strings.Replace(base,rec.RevisionID,strings.Repeat("c",64),1),http.StatusForbidden},
		{"stale-generation",strings.Replace(base,"generation=2","generation=9",1),http.StatusForbidden},
		{"delete-action",strings.Replace(base,"action=start","action=delete",1),http.StatusForbidden},
		{"missing-workload",strings.Replace(base,"workload="+rec.WorkloadID,"workload=",1),http.StatusBadRequest},
		{"ambiguous-workload",base+"&workload=other",http.StatusBadRequest},
	}
	for _,tc:=range cases{
		t.Run(tc.name,func(t *testing.T){
			resp,err:=client.Get(tc.path)
			if err!=nil{t.Fatal(err)}
			resp.Body.Close()
			if resp.StatusCode!=tc.status{t.Fatalf("unexpected peer check status %d, want %d",resp.StatusCode,tc.status)}
		})
	}
	req,err:=http.NewRequest(http.MethodPost,base,strings.NewReader(`{"uid":0,"write":true}`))
	if err!=nil{t.Fatal(err)}
	resp,err:=client.Do(req)
	if err!=nil{t.Fatal(err)}
	resp.Body.Close()
	if resp.StatusCode!=http.StatusMethodNotAllowed {
		t.Fatalf("read-only broker accepted write: %d",resp.StatusCode)
	}
	policy,err:=loadOwnerFile(filepath.Join(filepath.Dir(socket),"..","not-present"),owner)
	_ = policy
	if err==nil{t.Fatal("untrusted absent path treated as host policy")}
}

func TestPeerEnrollmentRequiresActualUIDNotCallerValue(t *testing.T) {
	dir,_,owner,rec,_:=readOnlyPeerFixture(t)
	policy,err:=loadOwnerFile(dir,owner)
	if err!=nil{t.Fatal(err)}
	selected:=SelectedRevision{DeploymentID:rec.DeploymentID,Project:rec.IncusProject,
		WorkloadID:rec.WorkloadID,RevisionID:rec.RevisionID,Generation:rec.Generation}
	if !peerUIDEnrolled(policy,rec.UID,selected,"start") {
		t.Fatal("matching kernel identity denied")
	}
	if peerUIDEnrolled(policy,rec.UID+1,selected,"start") ||
		peerUIDEnrolled(policy,rec.UID,selected,"delete") {
		t.Fatal("caller identity or unknown action bypassed peer policy")
	}
	// A matching UID is deliberately insufficient as a TLS or writer fence:
	// this helper returns only a Boolean used for a NOT-AUTHORIZED read view.
}

func TestPeerEnrollmentSocketRefusesAdoptionAndPreservesReplacements(t *testing.T) {
	dir,socket,owner,_,svc:=readOnlyPeerFixture(t)
	if _,err:=startPeerEnrollmentAt(context.Background(),dir,socket,owner);err==nil{
		t.Fatal("existing socket adopted")
	}
	replacement:=filepath.Join(filepath.Dir(socket),"replace.sock")
	if err:=os.Rename(socket,replacement);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(socket,[]byte("foreign-sentinel"),0o600);err!=nil{t.Fatal(err)}
	if err:=svc.Close();err!=nil{t.Fatal(err)}
	raw,err:=os.ReadFile(socket)
	if err!=nil||string(raw)!="foreign-sentinel"{
		t.Fatalf("peer service removed foreign replacement: %q %v",raw,err)
	}
	if err:=os.Remove(replacement);err!=nil{t.Fatal(err)}
}

func TestPeerEnrollmentServiceDoesNotStartOnCanceledContext(t *testing.T) {
	dir,socket,owner,_,svc:=readOnlyPeerFixture(t)
	if err:=svc.Close();err!=nil{t.Fatal(err)}
	ctx,cancel:=context.WithCancel(context.Background())
	cancel()
	if next,err:=startPeerEnrollmentAt(ctx,dir,socket,owner);next!=nil||err!=context.Canceled{
		t.Fatalf("canceled service started: %+v %v",next,err)
	}
	if _,err:=os.Lstat(socket);!os.IsNotExist(err){
		t.Fatalf("canceled start wrote socket: %v",err)
	}
}
