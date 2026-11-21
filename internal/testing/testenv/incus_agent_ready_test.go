package testenv

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type readinessFakeIncusClient struct {
	*fakeIncusVMClient
	probes     int
	readyAfter int
	argv       []string
	response   VMExecResult
}

func (c *readinessFakeIncusClient) ExecGuest(_ context.Context, _ string, argv []string) (VMExecResult, error) {
	c.probes++
	c.argv = append([]string(nil), argv...)
	if c.probes < c.readyAfter {
		return VMExecResult{}, errors.New("synthetic guest agent unavailable; private data")
	}
	return c.response, nil
}

func TestIncusAgentReadinessAfterNativeStart(t *testing.T) {
	client := &readinessFakeIncusClient{
		fakeIncusVMClient: newFakeIncusClient(t),
		readyAfter:        3,
	}
	backend, err := newTestIncusBackend(client, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := backend.CreateReadyIncusVM(context.Background(), admittedSpec(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.verifyRef(ref); err != nil {
		t.Fatal(err)
	}
	if client.probes != 3 || len(client.argv) != 1 || client.argv[0] != incusAgentProbeExecutable {
		t.Fatalf("agent probe not bounded and fixed: attempts=%d argv=%q", client.probes, client.argv)
	}
	if vm, ok := client.vms[ref.Instance]; !ok || !vm.Ephemeral || vm.Status != "Running" {
		t.Fatal("successful readiness lost disposable owned VM")
	}
}

func TestIncusAgentTimeoutPreservesRecoverableHandle(t *testing.T) {
	client := &readinessFakeIncusClient{
		fakeIncusVMClient: newFakeIncusClient(t),
		readyAfter:        1000000,
	}
	backend, _ := newTestIncusBackend(client, "testenv")
	ref, err := backend.CreateReadyIncusVM(context.Background(), admittedSpec(), 150*time.Millisecond)
	if !errors.Is(err, ErrIncusAgentNotReady) {
		t.Fatalf("missing safe timeout classification: %v", err)
	}
	if ref.RunID == "" || backend.verifyRef(ref) != nil {
		t.Fatalf("lost sealed recoverable run handle: %#v", ref)
	}
	if _, exists := client.vms[ref.Instance]; !exists {
		t.Fatal("failed agent readiness implicitly deleted an owned VM")
	}
	if client.probes < 1 || client.probes > 3 {
		t.Fatalf("unexpected probe attempts: %d", client.probes)
	}
}

func TestIncusAgentRefusesTamperedRunAndStoppedVM(t *testing.T) {
	client := &readinessFakeIncusClient{fakeIncusVMClient: newFakeIncusClient(t)}
	backend, _ := newTestIncusBackend(client, "testenv")
	ref, err := backend.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	foreign := ref
	foreign.ImageFingerprint = strings.Repeat("f", 64)
	if err := backend.WaitForIncusVMAgent(context.Background(), foreign, time.Second); !errors.Is(err, ErrOwnershipMismatch) {
		t.Fatalf("tampered run admitted: %v", err)
	}
	if client.probes != 0 {
		t.Fatal("guest probe reached foreign run")
	}
	vm := client.vms[ref.Instance]
	vm.Status = "Stopped"
	client.vms[ref.Instance] = vm
	if err := backend.WaitForIncusVMAgent(context.Background(), ref, time.Second); !errors.Is(err, ErrIncusAgentNotReady) {
		t.Fatalf("stopped VM reported ready: %v", err)
	}
	if client.probes != 0 {
		t.Fatal("stopped VM received probe")
	}
}

func TestIncusAgentNoExecCapabilityDoesNotProvision(t *testing.T) {
	client := newFakeIncusClient(t) // no GuestExecClient
	backend, _ := newTestIncusBackend(client, "testenv")
	ref, err := backend.CreateReadyIncusVM(context.Background(), admittedSpec(), time.Second)
	if !errors.Is(err, ErrUnsupportedAction) || ref.RunID != "" || len(client.vms) != 0 {
		t.Fatalf("unusable worker launched guest: %#v %v", ref, err)
	}
}

func TestIncusAgentRejectsBadBudgetAndCancellation(t *testing.T) {
	client := &readinessFakeIncusClient{fakeIncusVMClient: newFakeIncusClient(t), readyAfter: 100000}
	backend, _ := newTestIncusBackend(client, "testenv")
	for _, wait := range []time.Duration{0, -time.Second, MaxIncusAgentReadiness + time.Second} {
		if ref, err := backend.CreateReadyIncusVM(context.Background(), admittedSpec(), wait); err == nil || ref.RunID != "" {
			t.Fatalf("unbounded agent wait admitted: %v %#v", err, ref)
		}
	}
	ref, err := backend.Create(context.Background(), admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := backend.WaitForIncusVMAgent(ctx, ref, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if client.probes != 0 {
		t.Fatal("cancelled context reached guest agent")
	}
}
