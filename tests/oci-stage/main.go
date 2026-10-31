// Test-only OCI staging. This executable is never installed in the production CLI.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	compose "github.com/lxc/incus-compose/client"
	"github.com/lxc/incus-compose/iclient"
	"github.com/lxc/incus/v7/shared/api"
)

//go:embed pins.json
var pinsJSON []byte

const project = "aginctus-gateway"

type platformPin struct {
	Manifest         string `json:"manifest"`
	Config           string `json:"config"`
	CompressedBytes  int64  `json:"compressedBytes"`
	IncusFingerprint string `json:"incusFingerprint"`
}

type imagePin struct {
	Name       string                 `json:"name"`
	Repository string                 `json:"repository"`
	SourceTag  string                 `json:"sourceTag"`
	Index      string                 `json:"index"`
	Platforms  map[string]platformPin `json:"platforms"`
}

type manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	VerifiedAt    string     `json:"verifiedAt"`
	Images        []imagePin `json:"images"`
}

func loadPins() (manifest, error) {
	var m manifest
	d := json.NewDecoder(bytes.NewReader(pinsJSON))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	if m.SchemaVersion != 1 || len(m.Images) != 2 {
		return m, errors.New("unsupported test pin manifest")
	}
	return m, nil
}

func ownership() api.ConfigMap {
	return api.ConfigMap{
		"user.aginctus.managed": "true", "user.aginctus.resource": "compose-project",
		"user.aginctus.deployment": project,
	}
}

func checkProject(config api.ConfigMap) error {
	for k, v := range ownership() {
		if config[k] != v {
			return errors.New("refusing foreign gateway project")
		}
	}
	if config["features.images"] == "false" {
		return errors.New("gateway project must have isolated images")
	}
	return nil
}

func checkImage(img *api.Image, pin platformPin, arch string) error {
	wantArch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[arch]
	if img.Fingerprint != pin.IncusFingerprint || img.Architecture != wantArch {
		return errors.New("staged image fingerprint or architecture mismatch")
	}
	for _, key := range []string{"oci.entrypoint", "oci.cmd", "oci.cwd", "oci.volumes"} {
		if _, ok := img.Properties[key]; !ok {
			return fmt.Errorf("staged image missing %s; refusing incomplete OCI cache", key)
		}
	}
	return nil
}

func stage(ctx context.Context, m manifest, create bool) error {
	// The marker prevents accidental use; it is not a security boundary against
	// guest root. Only the dedicated worker provides host isolation.
	marker, err := os.ReadFile("/etc/aginctus-test-oci")
	if err != nil || string(marker) != "disposable-guest-only\n" {
		return errors.New("requires disposable OCI guest module")
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return errors.New("unsupported staging platform")
	}
	conn, err := iclient.NewConnection(&iclient.ConfigRemoteInfo{
		Name: "local", Addrs: []string{"unix:///var/lib/incus/unix.socket"}, Protocol: "incus",
	})
	if err != nil {
		return err
	}
	server, _, err := conn.GetServer(ctx)
	if err != nil {
		return err
	}
	if server.Config["core.https_address"] != "127.0.0.1:8443" {
		return errors.New("requires guest loopback-only Incus HTTPS 127.0.0.1:8443")
	}
	if create {
		if err := verifyRegistry(ctx, m); err != nil {
			return fmt.Errorf("registry metadata preflight: %w", err)
		}
	}
	p, _, err := conn.GetProject(ctx, project)
	if create && api.StatusErrorCheck(err, http.StatusNotFound) {
		config := ownership()
		config["features.profiles"] = "true"
		config["features.images"] = "true"
		if err := conn.CreateProject(ctx, api.ProjectsPost{Name: project, ProjectPut: api.ProjectPut{Config: config}}); err != nil {
			return err
		}
		p, _, err = conn.GetProject(ctx, project)
	}
	if err != nil {
		return err
	}
	if err := checkProject(p.Config); err != nil {
		return err
	}
	// Read-only check deliberately avoids GlobalClient.Connect/EnsureProject:
	// those may create infrastructure even when no image pull is requested.
	var c *compose.Client
	if create {
		g := compose.New(ctx, compose.ClientProvideConnection(conn))
		*g.CliConfig() = *iclient.ConfigDefaultConfig()
		if err := g.Connect(); err != nil {
			return err
		}
		c, err = g.EnsureProject(project)
		if err != nil {
			return err
		}
		defer c.WarnError(c.Done, "test OCI staging cleanup failed")
	}
	for _, image := range m.Images {
		pin, ok := image.Platforms[runtime.GOARCH]
		if !ok {
			return errors.New("platform absent from manifest")
		}
		ref := image.Repository + "@" + pin.Manifest
		alias := compose.ImageAlias(ref)
		if create {
			r, err := c.Resource(compose.KindImage, ref, &compose.ImageConfig{Platform: "linux/" + runtime.GOARCH})
			if err != nil {
				return err
			}
			if err := compose.RunAction(ctx, r, compose.ActionEnsure, compose.OptionCreate()); err != nil {
				return err
			}
		}
		a, _, err := conn.GetImageAlias(ctx, project, alias, nil)
		if err != nil {
			return err
		}
		img, _, err := conn.GetImage(ctx, project, a.Target, nil)
		if err != nil {
			return err
		}
		if err := checkImage(img, pin, runtime.GOARCH); err != nil {
			return fmt.Errorf("%s: %w", image.Name, err)
		}
	}
	return nil
}

func run(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: oci-stage plan|verify|stage|check")
	}
	m, err := loadPins()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	switch args[0] {
	case "plan":
		return json.NewEncoder(os.Stdout).Encode(m)
	case "verify":
		err = verifyRegistry(ctx, m)
	case "stage", "check":
		err = stage(ctx, m, args[0] == "stage")
	default:
		return errors.New("usage: oci-stage plan|verify|stage|check")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"action": args[0], "status": "passed", "manifestSHA256": fmt.Sprintf("%x", sha256.Sum256(pinsJSON)),
		"architecture": runtime.GOARCH, "offlineInference": "not_verified", "cleanup": "not_verified",
	})
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
