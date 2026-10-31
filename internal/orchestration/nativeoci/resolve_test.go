package nativeoci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type metadataFixture struct {
	top   ocispec.Descriptor
	blobs map[digest.Digest][]byte
	reads []string
	err   error
}

func (f *metadataFixture) FetchReference(_ context.Context, ref string) (ocispec.Descriptor, io.ReadCloser, error) {
	f.reads = append(f.reads, ref)
	return f.top, io.NopCloser(bytes.NewReader(f.blobs[f.top.Digest])), f.err
}

func (f *metadataFixture) Fetch(_ context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	f.reads = append(f.reads, desc.Digest.String())
	return io.NopCloser(bytes.NewReader(f.blobs[desc.Digest])), f.err
}

func (f *metadataFixture) add(mediaType string, value any) ocispec.Descriptor {
	body, _ := json.Marshal(value)
	desc := ocispec.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(body), Size: int64(len(body))}
	f.blobs[desc.Digest] = body
	return desc
}

func ociFixture(indexed bool, env string) (Source, *metadataFixture, ocispec.Descriptor) {
	source := Source{Registry: "registry.example", Repository: "gateway/litellm", Reference: "stable", Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"}}
	fixture := &metadataFixture{blobs: map[digest.Digest][]byte{}}
	config := fixture.add(ocispec.MediaTypeImageConfig, ocispec.Image{
		Platform: source.Platform, Config: ocispec.ImageConfig{
			Env: []string{env}, Entrypoint: []string{"/entrypoint"}, Cmd: []string{"--port", "4000"}, User: "1000:1000", WorkingDir: "/app", Volumes: map[string]struct{}{"/data": {}},
		},
	})
	manifest := fixture.add(ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, Config: config,
		Layers: []ocispec.Descriptor{{Digest: digest.FromString("layer-one"), Size: 100}, {Digest: digest.FromString("layer-two"), Size: 200}},
	})
	fixture.top = manifest
	if indexed {
		candidate := manifest
		candidate.Platform = &source.Platform
		fixture.top = fixture.add(ocispec.MediaTypeImageIndex, ocispec.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: []ocispec.Descriptor{candidate}})
	}
	return source, fixture, manifest
}

func TestOCIImmutableIdentityAndRuntimeMetadata(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		source, fixture, manifest := ociFixture(indexed, "SENSITIVE=registry-config")
		resolved, err := ResolveWithRepository(context.Background(), source, fixture)
		if err != nil || resolved.ManifestDigest != manifest.Digest.String() || len(resolved.IncusFingerprint) != 64 {
			t.Fatalf("unexpected immutable identity: %+v", err)
		}
		if resolved.Config.User != "1000:1000" || resolved.Config.WorkingDir != "/app" || len(resolved.Config.Entrypoint) != 1 || len(resolved.Config.Cmd) != 2 || len(resolved.Config.Volumes) != 1 {
			t.Fatal("OCI initialization metadata lost")
		}
		encoded, _ := json.Marshal(resolved)
		if strings.Contains(string(encoded), "SENSITIVE") || strings.Contains(string(encoded), "registry-config") {
			t.Fatal("serialized image identity exposed runtime configuration")
		}
		// Pinning the selected manifest must yield the same identity without
		// consulting the mutable tag/index again.
		source.Reference = manifest.Digest.String()
		fixture.top = manifest
		pinned, err := ResolveWithRepository(context.Background(), source, fixture)
		if err != nil || pinned.ManifestDigest != resolved.ManifestDigest || pinned.IncusFingerprint != resolved.IncusFingerprint {
			t.Fatal("pinned lookup changed image identity")
		}
	}
	firstSource, first, _ := ociFixture(false, "CONFIG=first")
	secondSource, second, _ := ociFixture(false, "CONFIG=second")
	a, errA := ResolveWithRepository(context.Background(), firstSource, first)
	b, errB := ResolveWithRepository(context.Background(), secondSource, second)
	if errA != nil || errB != nil || a.ManifestDigest == b.ManifestDigest || a.ConfigDigest == b.ConfigDigest || a.IncusFingerprint != b.IncusFingerprint {
		t.Fatal("config-only identity must differ despite equal Incus fingerprints")
	}
}

func TestOCIMetadataRejectsUntrustedIdentity(t *testing.T) {
	for _, mode := range []string{"credentials", "implicit reference", "embedded tag", "wrong platform", "ambiguous platform", "wrong config platform", "corrupt manifest", "oversized descriptor", "wrong pinned digest", "registry diagnostics"} {
		t.Run(mode, func(t *testing.T) {
			source, fixture, manifest := ociFixture(true, "CONFIG=synthetic")
			switch mode {
			case "credentials":
				source.Registry = "user:sensitive-password@registry.example"
			case "implicit reference":
				source.Reference = ""
			case "embedded tag":
				source.Repository += ":other"
			case "wrong platform":
				source.Platform.Architecture = "arm64"
			case "ambiguous platform":
				manifest.Platform = &source.Platform
				fixture.top = fixture.add(ocispec.MediaTypeImageIndex, ocispec.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: []ocispec.Descriptor{manifest, manifest}})
			case "wrong config platform":
				fixture.top = manifest
				source.Platform.Architecture = "arm64"
			case "corrupt manifest":
				fixture.blobs[fixture.top.Digest] = []byte(`{"sensitive":"registry response"}`)
			case "oversized descriptor":
				fixture.top.Size = maxMetadata + 1
			case "wrong pinned digest":
				source.Reference = "sha256:" + strings.Repeat("a", 64)
			case "registry diagnostics":
				fixture.err = errors.New("sensitive registry password")
			}
			_, err := ResolveWithRepository(context.Background(), source, fixture)
			if err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("unsafe registry metadata accepted or diagnostics leaked")
			}
		})
	}
	source, fixture, _ := ociFixture(false, "CONFIG=synthetic")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveWithRepository(ctx, source, fixture); !errors.Is(err, context.Canceled) || len(fixture.reads) != 0 {
		t.Fatal("canceled resolution performed registry reads or lost cancellation")
	}
}

func TestOCIMetadataRejectsInsecureAuthTransport(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://registry.example/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (httpsOnlyTransport{}).RoundTrip(request); err == nil {
		t.Fatal("insecure token realm accepted")
	}
}
