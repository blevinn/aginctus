package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lxc/incus-compose/iclient"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type referenceFetcher interface {
	FetchReference(context.Context, string) (ocispec.Descriptor, io.ReadCloser, error)
}

// fetchJSON bounds metadata reads and verifies bytes independently of registry
// headers. Verification downloads no layers and never loads registry auth.
func fetchJSON(ctx context.Context, repo referenceFetcher, ref string, out any) error {
	desc, rc, err := repo.FetchReference(ctx, ref)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 4<<20 || int64(len(body)) != desc.Size {
		return errors.New("OCI metadata size mismatch or limit exceeded")
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if digest != ref || digest != desc.Digest.String() {
		return errors.New("OCI metadata digest mismatch")
	}
	return json.Unmarshal(body, out)
}

func verifyManifest(doc ocispec.Manifest, pin platformPin) error {
	if doc.Config.Digest.String() != pin.Config {
		return errors.New("config descriptor differs from pin")
	}
	h := sha256.New()
	var size int64
	for _, layer := range doc.Layers {
		_, _ = io.WriteString(h, layer.Digest.String())
		if layer.Size < 0 || layer.Size > 1<<30 {
			return errors.New("layer size outside test budget")
		}
		size += layer.Size
	}
	if size != pin.CompressedBytes || size > 512<<20 || fmt.Sprintf("%x", h.Sum(nil)) != pin.IncusFingerprint {
		return errors.New("layer size or Incus fingerprint differs from pin")
	}
	return nil
}

func verifyRegistry(ctx context.Context, m manifest) error {
	for _, image := range m.Images {
		host, path, _ := strings.Cut(image.Repository, "/")
		url := "https://" + host
		if host == "docker.io" {
			url = "https://registry-1.docker.io"
		}
		repo, err := iclient.NewRepository(&iclient.ConfigRemoteInfo{
			Name: host, Addrs: []string{url}, Protocol: "oci", Public: true,
		}, path+"@"+image.Index)
		if err != nil {
			return err
		}
		var index ocispec.Index
		if err := fetchJSON(ctx, repo, image.Index, &index); err != nil {
			return err
		}
		for _, arch := range []string{"amd64", "arm64"} {
			pin := image.Platforms[arch]
			found := false
			for _, d := range index.Manifests {
				if d.Digest.String() == pin.Manifest && d.Platform != nil && d.Platform.OS == "linux" && d.Platform.Architecture == arch {
					found = true
				}
			}
			if !found {
				return errors.New("platform manifest absent from pinned index")
			}
			var doc ocispec.Manifest
			if err := fetchJSON(ctx, repo, pin.Manifest, &doc); err != nil {
				return err
			}
			if err := verifyManifest(doc, pin); err != nil {
				return err
			}
			var config ocispec.Image
			if err := fetchJSON(ctx, repo.Blobs(), pin.Config, &config); err != nil {
				return err
			}
			if config.OS != "linux" || config.Architecture != arch {
				return errors.New("config platform differs from pin")
			}
		}
	}
	return nil
}
