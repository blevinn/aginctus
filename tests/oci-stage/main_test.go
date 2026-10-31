package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	compose "github.com/lxc/incus-compose/client"
	"github.com/lxc/incus/v7/shared/api"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestManifestAndAliases(t *testing.T) {
	m, err := loadPins()
	if err != nil {
		t.Fatal(err)
	}
	aliases := map[string]bool{}
	for _, image := range m.Images {
		if err := digest.Digest(image.Index).Validate(); err != nil {
			t.Fatal(err)
		}
		for _, arch := range []string{"amd64", "arm64"} {
			pin := image.Platforms[arch]
			for _, d := range []string{pin.Manifest, pin.Config, "sha256:" + pin.IncusFingerprint} {
				if err := digest.Digest(d).Validate(); err != nil {
					t.Fatal(err)
				}
			}
			ref := image.Repository + "@" + pin.Manifest
			c := compose.NewOfflineClient(t.Context(), project)
			r, err := c.Resource(compose.KindImage, ref, &compose.ImageConfig{Platform: "linux/" + arch})
			if err != nil {
				t.Fatal(err)
			}
			alias := compose.ImageAlias(ref)
			if r.IncusName() != alias || len(alias) > 64 || aliases[alias] {
				t.Fatal("staging alias must match pinned Compose normalization and be unique")
			}
			aliases[alias] = true
			if pin.CompressedBytes <= 0 || pin.CompressedBytes > 512<<20 {
				t.Fatal("pin outside compressed download budget")
			}
		}
	}
}

func TestRejectForeignProject(t *testing.T) {
	if err := checkProject(ownership()); err != nil {
		t.Fatal(err)
	}
	for k := range ownership() {
		config := ownership()
		delete(config, k)
		if checkProject(config) == nil {
			t.Fatal("accepted foreign project")
		}
	}
	config := ownership()
	config["features.images"] = "false"
	if checkProject(config) == nil {
		t.Fatal("accepted shared default-project images")
	}
}

func TestRejectIncompleteOrWrongCache(t *testing.T) {
	m, err := loadPins()
	if err != nil {
		t.Fatal(err)
	}
	pin := m.Images[0].Platforms["amd64"]
	img := &api.Image{Fingerprint: pin.IncusFingerprint, Architecture: "x86_64", ImagePut: api.ImagePut{
		Properties: map[string]string{"oci.entrypoint": "", "oci.cmd": "", "oci.cwd": "/", "oci.volumes": ","},
	}}
	if err := checkImage(img, pin, "amd64"); err != nil {
		t.Fatal(err)
	}
	img.Fingerprint = "wrong"
	if checkImage(img, pin, "amd64") == nil {
		t.Fatal("accepted poisoned alias")
	}
	img.Fingerprint = pin.IncusFingerprint
	if checkImage(img, pin, "arm64") == nil {
		t.Fatal("accepted wrong architecture")
	}
	delete(img.Properties, "oci.volumes")
	if checkImage(img, pin, "amd64") == nil {
		t.Fatal("accepted cache that could trigger registry metadata fallback")
	}
}

type fixtureFetcher struct {
	body []byte
	size int64
}

func (f fixtureFetcher) FetchReference(_ context.Context, ref string) (ocispec.Descriptor, io.ReadCloser, error) {
	return ocispec.Descriptor{Digest: digest.Digest(ref), Size: f.size}, io.NopCloser(bytes.NewReader(f.body)), nil
}

func TestMetadataDigestAndSize(t *testing.T) {
	body := []byte(`{"schemaVersion":2}`)
	ref := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	for _, tc := range []struct {
		name string
		body []byte
		size int64
		ok   bool
	}{
		{"valid", body, int64(len(body)), true},
		{"tampered", []byte(`{"schemaVersion":3}`), int64(len(body)), false},
		{"size", body, 1, false},
		{"oversized", bytes.Repeat([]byte("x"), (4<<20)+1), (4 << 20) + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out any
			err := fetchJSON(t.Context(), fixtureFetcher{tc.body, tc.size}, ref, &out)
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected metadata validation: %v", err)
			}
		})
	}
}

func TestLayerFingerprintAndBudget(t *testing.T) {
	var doc ocispec.Manifest
	if err := json.Unmarshal([]byte(`{"config":{"digest":"sha256:config"},"layers":[{"digest":"sha256:layer","size":10}]}`), &doc); err != nil {
		t.Fatal(err)
	}
	pin := platformPin{Config: "sha256:config", CompressedBytes: 10, IncusFingerprint: fmt.Sprintf("%x", sha256.Sum256([]byte("sha256:layer")))}
	if err := verifyManifest(doc, pin); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int64{-1, 11, 513 << 20, 2 << 30} {
		doc.Layers[0].Size = size
		if verifyManifest(doc, pin) == nil {
			t.Fatal("accepted size drift or unbounded layer")
		}
	}
	doc.Layers[0].Size = 10
	doc.Layers[0].Digest = "sha256:other"
	if verifyManifest(doc, pin) == nil {
		t.Fatal("accepted layer identity drift")
	}
}
