package herdr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"testing"
)

type fakeGuest struct {
	arch string
	exists bool
	written []byte
	path string
	execs [][]string
}

func (g *fakeGuest) InstanceArchitecture(context.Context, string) (string, error) { return g.arch, nil }
func (g *fakeGuest) WriteInstanceFile(_ context.Context, _ string, path string, data []byte, _ int) error {
	g.path = path
	g.written = append([]byte(nil), data...)
	return nil
}
func (g *fakeGuest) ExecInstance(_ context.Context, _ string, command []string) (string, error) {
	g.execs = append(g.execs, append([]string(nil), command...))
	if len(command) > 0 && command[0] == installPath {
		if g.exists || len(g.written) > 0 {
			return "herdr 0.9.1\n", nil
		}
		return "", io.EOF
	}
	return "", nil
}

type roundTripper func(*http.Request) (*http.Response, error)
func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEnsureInstallsVerifiedBinary(t *testing.T) {
	payload := []byte("herdr-binary")
	sum := sha256.Sum256(payload)
	guest := &fakeGuest{arch: "x86_64"}
	installer := &Installer{Client: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(payload))}, nil
	})}}

	err := installer.Ensure(context.Background(), guest, "aginctus-herdr", Release{
		Repository: "herdrdev/herdr",
		Version: "v0.9.1",
		SHA256X8664: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if guest.path != installPath || !bytes.Equal(guest.written, payload) {
		t.Fatalf("path = %q written = %q", guest.path, guest.written)
	}
}

func TestEnsureSkipsDownloadWhenInstalled(t *testing.T) {
	guest := &fakeGuest{arch: "x86_64", exists: true}
	installer := &Installer{Client: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		t.Fatal("unexpected download")
		return nil, nil
	})}}
	if err := installer.Ensure(context.Background(), guest, "aginctus-herdr", Release{}); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
}
