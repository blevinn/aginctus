package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	gatewayassets "github.com/blevinn/aginctus/scripts/gateway"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const siwcFrameLimit = 2 << 20

type CredentialUpdate func(json.RawMessage) (json.RawMessage, error)

// ExchangeSIWC keeps credentials on authenticated exec streams, never argv,
// environment, retained output, host files or the workload network.
func ExchangeSIWC(ctx context.Context, attachment CredentialAttachment, update CredentialUpdate) error {
	return exchangeSIWCOnHost(ctx, attachment, update, false)
}

func exchangeSIWCOnHost(ctx context.Context, attachment CredentialAttachment, update CredentialUpdate, prepare bool) error {
	if err := AuditCredentialAttachment(ctx, attachment); err != nil {
		return err
	}
	client, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("connect to managed SIWC gateway")
	}
	server := client.UseProject(attachment.ComposeProject)
	instance, _, err := server.GetInstance(liteLLMServiceInstance)
	if err != nil {
		return fmt.Errorf("inspect managed SIWC gateway")
	}
	if err := ownedLiteLLM(instance, attachment.ComposeProject); err != nil {
		return err
	}
	input, writer := io.Pipe()
	reader, output := io.Pipe()
	defer input.Close()
	defer writer.Close()
	defer reader.Close()
	defer output.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = writer.CloseWithError(ctx.Err())
			_ = reader.CloseWithError(ctx.Err())
		case <-finished:
		}
	}()
	done := make(chan bool)
	op, err := server.ExecInstance(liteLLMServiceInstance, api.InstanceExecPost{Command: siwcAdminCommand(prepare), WaitForWS: true}, &incusclient.InstanceExecArgs{Stdin: input, Stdout: output, Stderr: io.Discard, DataDone: done})
	if err != nil {
		return fmt.Errorf("start managed SIWC credential exchange")
	}
	go func() { <-done; _ = output.Close() }()
	result := exchangeSIWC(reader, writer, update)
	_ = writer.Close()
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("managed SIWC credential exchange did not complete")
	}
	code, ok := op.Get().Metadata["return"].(float64)
	if !ok || code != 0 {
		return fmt.Errorf("managed SIWC credential exchange failed; inspect gateway readiness and retry apply/login")
	}
	return result
}

func siwcAdminCommand(prepare bool) []string {
	files := gatewayassets.SIWCFiles()
	runtime, _ := json.Marshal(files["siwc.py"])
	helper, _ := json.Marshal(files["siwc_admin.py"])
	code := fmt.Sprintf("import sys,types; m=types.ModuleType('siwc'); sys.modules['siwc']=m; exec(%s,m.__dict__); exec(%s)", runtime, helper)
	mode := "active"
	if prepare {
		mode = "prepare"
	}
	return []string{"python3", "-c", code, mode}
}

func exchangeSIWC(reader io.Reader, writer io.Writer, update CredentialUpdate) error {
	decoder := json.NewDecoder(io.LimitReader(reader, siwcFrameLimit+1024))
	var envelope struct {
		State      json.RawMessage `json:"state"`
		BaseDigest string          `json:"base_digest"`
	}
	if err := decoder.Decode(&envelope); err != nil || len(envelope.State) == 0 || len(envelope.BaseDigest) != 64 {
		return fmt.Errorf("managed SIWC state unavailable or invalid")
	}
	replacement, actionErr := update(envelope.State)
	if replacement == nil {
		return actionErr
	}
	if len(replacement) > siwcFrameLimit || !json.Valid(replacement) {
		return fmt.Errorf("invalid managed SIWC replacement")
	}
	envelope.State = replacement
	if err := json.NewEncoder(writer).Encode(envelope); err != nil {
		return fmt.Errorf("managed SIWC update delivery failed")
	}
	var acknowledgment struct {
		OK bool `json:"ok"`
	}
	if err := decoder.Decode(&acknowledgment); err != nil || !acknowledgment.OK {
		return fmt.Errorf("managed SIWC update was not acknowledged; retry provider login")
	}
	return actionErr
}

// PrepareSIWC provisions the existing private volume and initializes it as the
// verified runtime user, then reacquires the worker lease on the mounted store.
func PrepareSIWC(ctx context.Context, attachment CredentialAttachment) error {
	if err := AttachCredentialVolume(ctx, attachment); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := exchangeSIWCOnHost(ctx, attachment, func(json.RawMessage) (json.RawMessage, error) { return nil, nil }, true)
		if err == nil {
			break
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("initialize managed SIWC storage: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	client, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("connect to restart managed SIWC owner")
	}
	op, err := client.UseProject(attachment.ComposeProject).UpdateInstanceState(liteLLMServiceInstance, api.InstanceStatePut{Action: "restart", Timeout: 30}, "")
	if err != nil {
		return fmt.Errorf("restart managed SIWC owner")
	}
	if err := op.WaitContext(ctx); err != nil {
		return err
	}
	deadline = time.Now().Add(30 * time.Second)
	for {
		err := ExchangeSIWC(ctx, attachment, func(json.RawMessage) (json.RawMessage, error) { return nil, nil })
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("managed SIWC owner did not become ready: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
