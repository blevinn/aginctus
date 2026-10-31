// Package nativeoci resolves OCI identity separately from Incus orchestration.
// Registry metadata is read with ORAS; Incus remains responsible for acquisition,
// conversion and instance lifecycle through the official SDK.
package nativeoci

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

const (
	dockerIndex    = "application/vnd.docker.distribution.manifest.list.v2+json"
	dockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	dockerConfig   = "application/vnd.docker.container.image.v1+json"
	maxMetadata    = 4 << 20
)

var (
	sha256Digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	imageTag     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

type Source struct {
	Registry   string
	Repository string
	Reference  string
	Platform   ocispec.Platform
}

// ResolvedImage keeps the complete manifest/config identity distinct from the
// layer-only Incus fingerprint. Config is runtime metadata, never plan output.
type ResolvedImage struct {
	Registry         string      `json:"registry"`
	Repository       string      `json:"repository"`
	ManifestDigest   string      `json:"manifestDigest"`
	ConfigDigest     string      `json:"configDigest"`
	IncusFingerprint string      `json:"incusFingerprint"`
	Config           ImageConfig `json:"-"`
}

type ImageConfig struct {
	ocispec.ImageConfig
	Healthcheck json.RawMessage `json:"Healthcheck,omitempty"`
}

// RepositoryReader is metadata-only; it cannot upload, tag, remove or unpack.
type RepositoryReader interface {
	FetchReference(context.Context, string) (ocispec.Descriptor, io.ReadCloser, error)
	Fetch(context.Context, ocispec.Descriptor) (io.ReadCloser, error)
}

func (source Source) Validate() error {
	u, err := url.Parse("https://" + source.Registry)
	if err != nil || source.Registry == "" || u.Host != source.Registry || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("OCI registry must be an explicit credential-free HTTPS host")
	}
	if source.Repository == "" || (!sha256Digest.MatchString(source.Reference) && !imageTag.MatchString(source.Reference)) {
		return fmt.Errorf("OCI repository and tag or SHA-256 digest must be explicit")
	}
	repo, err := remote.NewRepository(source.Registry + "/" + source.Repository)
	if err != nil || repo.Reference.Reference != "" || repo.Reference.Repository != source.Repository {
		return fmt.Errorf("invalid OCI repository identity")
	}
	if source.Platform.OS != "linux" || source.Platform.Architecture == "" {
		return fmt.Errorf("OCI platform must explicitly select Linux and an architecture")
	}
	return nil
}

// ResolvePublic uses anonymous registry authentication only. It never reads
// ambient Docker/Incus credential helpers or forwards operator credentials.
// Private-registry acquisition needs a separate explicit credential contract.
func ResolvePublic(ctx context.Context, source Source) (ResolvedImage, error) {
	if err := source.Validate(); err != nil {
		return ResolvedImage{}, err
	}
	repo, err := remote.NewRepository(source.Registry + "/" + source.Repository)
	if err != nil {
		return ResolvedImage{}, fmt.Errorf("invalid OCI repository identity")
	}
	repo.Client = &auth.Client{Cache: auth.NewCache(), Client: &http.Client{
		Timeout:   30 * time.Second,
		Transport: httpsOnlyTransport{},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 10 {
				return fmt.Errorf("OCI metadata redirect rejected")
			}
			return nil
		},
	}}
	return ResolveWithRepository(ctx, source, repo)
}

type httpsOnlyTransport struct{}

func (httpsOnlyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.User != nil {
		return nil, fmt.Errorf("OCI metadata transport requires credential-free HTTPS URLs")
	}
	return http.DefaultTransport.RoundTrip(request)
}

func ResolveWithRepository(ctx context.Context, source Source, repo RepositoryReader) (ResolvedImage, error) {
	if err := source.Validate(); err != nil {
		return ResolvedImage{}, err
	}
	if err := ctx.Err(); err != nil {
		return ResolvedImage{}, err
	}
	descriptor, reader, err := repo.FetchReference(ctx, source.Reference)
	if err != nil {
		return ResolvedImage{}, metadataFailure(ctx)
	}
	body, err := readMetadata(descriptor, reader)
	if err != nil {
		return ResolvedImage{}, metadataFailure(ctx)
	}
	if sha256Digest.MatchString(source.Reference) && descriptor.Digest.String() != source.Reference {
		return ResolvedImage{}, fmt.Errorf("OCI digest reference mismatch")
	}
	if descriptor.MediaType == ocispec.MediaTypeImageIndex || descriptor.MediaType == dockerIndex {
		var index ocispec.Index
		if err := json.Unmarshal(body, &index); err != nil || index.SchemaVersion != 2 {
			return ResolvedImage{}, fmt.Errorf("invalid OCI image index")
		}
		var matches []ocispec.Descriptor
		for _, candidate := range index.Manifests {
			if candidate.Platform != nil && samePlatform(source.Platform, *candidate.Platform) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) != 1 {
			return ResolvedImage{}, fmt.Errorf("OCI platform selection missing or ambiguous")
		}
		descriptor = matches[0]
		body, err = fetchMetadata(ctx, repo, descriptor)
		if err != nil {
			return ResolvedImage{}, err
		}
	}
	if descriptor.MediaType != ocispec.MediaTypeImageManifest && descriptor.MediaType != dockerManifest {
		return ResolvedImage{}, fmt.Errorf("unsupported OCI manifest type")
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil || manifest.SchemaVersion != 2 || len(manifest.Layers) == 0 {
		return ResolvedImage{}, fmt.Errorf("invalid OCI image manifest")
	}
	if manifest.Config.MediaType != ocispec.MediaTypeImageConfig && manifest.Config.MediaType != dockerConfig {
		return ResolvedImage{}, fmt.Errorf("unsupported OCI config type")
	}
	body, err = fetchMetadata(ctx, repo, manifest.Config)
	if err != nil {
		return ResolvedImage{}, err
	}
	var image struct {
		ocispec.Image
		Config ImageConfig `json:"config"`
	}
	if err := json.Unmarshal(body, &image); err != nil || !samePlatform(source.Platform, image.Platform) {
		return ResolvedImage{}, fmt.Errorf("OCI config platform mismatch or invalid config")
	}
	hash := sha256.New()
	for _, layer := range manifest.Layers {
		if !sha256Digest.MatchString(layer.Digest.String()) || layer.Size < 0 {
			return ResolvedImage{}, fmt.Errorf("unsupported OCI layer descriptor")
		}
		_, _ = hash.Write([]byte(layer.Digest.String()))
	}
	if err := ctx.Err(); err != nil {
		return ResolvedImage{}, err
	}
	return ResolvedImage{
		Registry: source.Registry, Repository: source.Repository,
		ManifestDigest: descriptor.Digest.String(), ConfigDigest: manifest.Config.Digest.String(),
		IncusFingerprint: fmt.Sprintf("%x", hash.Sum(nil)), Config: image.Config,
	}, nil
}

func samePlatform(want, got ocispec.Platform) bool {
	return platforms.OnlyStrict(platforms.Normalize(want)).Match(platforms.Normalize(got))
}

func fetchMetadata(ctx context.Context, repo RepositoryReader, descriptor ocispec.Descriptor) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Validate before asking a transport to fetch an untrusted descriptor.
	if !validDescriptor(descriptor) {
		return nil, fmt.Errorf("invalid OCI metadata descriptor")
	}
	reader, err := repo.Fetch(ctx, descriptor)
	if err != nil {
		return nil, metadataFailure(ctx)
	}
	body, err := readMetadata(descriptor, reader)
	if err != nil {
		return nil, metadataFailure(ctx)
	}
	return body, nil
}

func validDescriptor(descriptor ocispec.Descriptor) bool {
	return sha256Digest.MatchString(descriptor.Digest.String()) && descriptor.Size > 0 && descriptor.Size <= maxMetadata
}

func readMetadata(descriptor ocispec.Descriptor, reader io.ReadCloser) ([]byte, error) {
	defer func() { _ = reader.Close() }()
	if !validDescriptor(descriptor) {
		return nil, fmt.Errorf("invalid OCI metadata descriptor")
	}
	return content.ReadAll(reader, descriptor)
}

func metadataFailure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("OCI metadata lookup canceled: %w", err)
	}
	// Registry error bodies, URLs and OCI config can contain credentials.
	return fmt.Errorf("OCI metadata unavailable or failed integrity validation")
}
