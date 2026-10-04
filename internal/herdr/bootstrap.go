package herdr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const installPath = "/usr/local/bin/herdr"

type Guest interface {
	InstanceArchitecture(context.Context, string) (string, error)
	WriteInstanceFile(context.Context, string, string, []byte, int) error
	ExecInstance(context.Context, string, []string) (string, error)
}

type Release struct {
	Repository   string
	Version      string
	SHA256X8664  string
	SHA256AArch64 string
}

type Installer struct {
	Client *http.Client
}

func NewInstaller() *Installer {
	return &Installer{Client: http.DefaultClient}
}

func (i *Installer) Ensure(ctx context.Context, guest Guest, instanceName string, release Release) error {
	arch, err := guest.InstanceArchitecture(ctx, instanceName)
	if err != nil {
		return err
	}

	assetArch, expected, err := releaseForArchitecture(arch, release)
	if err != nil {
		return err
	}

	if _, err := guest.ExecInstance(ctx, instanceName, []string{installPath, "--version"}); err == nil {
		return nil
	}

	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/herdr-linux-%s", release.Repository, release.Version, assetArch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build Herdr download request: %w", err)
	}
	resp, err := i.Client.Do(req)
	if err != nil {
		return fmt.Errorf("download Herdr %s: %w", release.Version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Herdr %s: unexpected HTTP status %s", release.Version, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return fmt.Errorf("read Herdr release: %w", err)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("verify Herdr %s: sha256 %s, want %s", release.Version, actual, expected)
	}

	if _, err := guest.ExecInstance(ctx, instanceName, []string{"mkdir", "-p", "/usr/local/bin"}); err != nil {
		return fmt.Errorf("prepare Herdr install directory: %w", err)
	}
	if err := guest.WriteInstanceFile(ctx, instanceName, installPath, data, 0o755); err != nil {
		return fmt.Errorf("install Herdr binary: %w", err)
	}
	if output, err := guest.ExecInstance(ctx, instanceName, []string{installPath, "--version"}); err != nil {
		return fmt.Errorf("verify Herdr installation: %w", err)
	} else if strings.TrimSpace(output) == "" {
		return fmt.Errorf("verify Herdr installation: empty version output")
	}

	return nil
}

func releaseForArchitecture(arch string, release Release) (string, string, error) {
	switch arch {
	case "x86_64":
		return "x86_64", release.SHA256X8664, nil
	case "aarch64":
		return "aarch64", release.SHA256AArch64, nil
	default:
		return "", "", fmt.Errorf("unsupported Herdr client architecture %q", arch)
	}
}
