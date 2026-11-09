package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSIWCExchangeRequiresAcknowledgedCommit(t *testing.T) {
	for _, ack := range []string{`{"ok":true}`, `{"ok":false}`, ""} {
		input := `{"state":{"fixture":"synthetic"},"base_digest":"` + strings.Repeat("a", 64) + `"}` + "\n" + ack
		var output bytes.Buffer
		err := exchangeSIWC(strings.NewReader(input), &output, func(state json.RawMessage) (json.RawMessage, error) {
			if !bytes.Contains(state, []byte("synthetic")) {
				t.Fatal("lost authoritative state")
			}
			return json.RawMessage(`{"fixture":"updated"}`), nil
		})
		if (err == nil) != (ack == `{"ok":true}`) {
			t.Fatalf("acknowledgment %q: %v", ack, err)
		}
		if !strings.Contains(output.String(), `"fixture":"updated"`) {
			t.Fatal("update not delivered on stdin")
		}
	}
}

func TestSIWCExchangePreservesFailureStateWithoutLeakingDiagnostics(t *testing.T) {
	input := `{"state":{},"base_digest":"` + strings.Repeat("a", 64) + `"}` + "\n" + `{"ok":true}`
	want := errors.New("fixed operation diagnostic")
	var output bytes.Buffer
	err := exchangeSIWC(strings.NewReader(input), &output, func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"refresh_pending":true}`), want
	})
	if !errors.Is(err, want) || !strings.Contains(output.String(), "refresh_pending") {
		t.Fatal("discarded refresh recovery state")
	}
	if err := exchangeSIWC(strings.NewReader("synthetic-secret-not-json"), &output, func(json.RawMessage) (json.RawMessage, error) {
		t.Fatal("called action on invalid state")
		return nil, nil
	}); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("unsafe protocol diagnostic")
	}
}

func TestSIWCExchangeCommitsThroughRealPythonProtocol(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python protocol probe requires the development shell")
	}
	directory := filepath.Join(t.TempDir(), "credentials")
	command := exec.Command(python, "-c", `import sys;sys.path.insert(0,sys.argv[1]);import siwc,siwc_admin;siwc_admin.exchange(siwc.Store(sys.argv[2]),sys.stdin,sys.stdout)`, filepath.Join("..", "..", "scripts", "gateway"), directory)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	err = exchangeSIWC(output, input, func(raw json.RawMessage) (json.RawMessage, error) {
		var state map[string]any
		if err := json.Unmarshal(raw, &state); err != nil {
			return nil, err
		}
		state["accounts"] = map[string]any{"personal": map[string]any{"client_id": "oaiapp_fixture", "scopes": []string{}}}
		return json.Marshal(state)
	})
	_ = input.Close()
	if err != nil {
		_ = command.Wait()
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if err != nil || !bytes.Contains(state, []byte("oaiapp_fixture")) {
		t.Fatalf("authoritative state was not committed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "pending.json")); !os.IsNotExist(err) {
		t.Fatal("pending state was not consumed")
	}
}
