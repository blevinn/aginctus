package testenv

import (
	"errors"
	"strings"
	"testing"
)

func TestIncusNativeOutputBoundedAcrossBothStreams(t *testing.T) {
	buffer := &guestOutputBuffer{}
	stdout := guestStreamWriter{buffer: buffer, stdout: true}
	stderr := guestStreamWriter{buffer: buffer}
	if n, err := stdout.Write([]byte("output")); err != nil || n != 6 {
		t.Fatalf("stdout write: %d %v", n, err)
	}
	if n, err := stderr.Write([]byte("error")); err != nil || n != 5 {
		t.Fatalf("stderr write: %d %v", n, err)
	}
	result, err := buffer.Result(5)
	if err != nil || result.ExitCode != 5 || result.Stdout != "output" || result.Stderr != "error" {
		t.Fatalf("lost stream separation or exit code: %#v %v", result, err)
	}
	huge := strings.Repeat("a", MaxGuestOutput)
	if n, err := stdout.Write([]byte(huge)); err != nil || n != len(huge) {
		t.Fatalf("oversized stream blocked: %d %v", n, err)
	}
	if _, err := buffer.Result(0); !errors.Is(err, ErrGuestOutputLimit) {
		t.Fatalf("stream overflow reported successful: %v", err)
	}
	if buffer.stdout.Len()+buffer.stderr.Len() > MaxGuestOutput {
		t.Fatal("unbounded guest stdout/stderr allocated")
	}
}

func TestIncusNativeOutputAllowsExactBoundary(t *testing.T) {
	buffer := &guestOutputBuffer{}
	writer := guestStreamWriter{buffer: buffer, stdout: true}
	_, _ = writer.Write([]byte(strings.Repeat("x", MaxGuestOutput)))
	_, err := buffer.Result(0)
	if err != nil {
		t.Fatalf("exact output limit rejected: %v", err)
	}
}
