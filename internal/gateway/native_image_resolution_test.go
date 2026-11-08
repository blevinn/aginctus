package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestResolveNativeGatewayImagesPinsBothSources(t *testing.T) {
	p := nativePinnedFixture()
	p.Services[0].ImageReference = "docker.io/library/postgres:16"
	p.Services[1].ImageReference = "ghcr.io/berriai/litellm:main"
	sources := NativeGatewayImageSources{
		Postgres: nativeoci.Source{Registry:"docker.io", Repository:"library/postgres", Reference:"16", Platform:ocispec.Platform{OS:"linux",Architecture:"amd64"}},
		LiteLLM: nativeoci.Source{Registry:"ghcr.io",Repository:"berriai/litellm",Reference:"main", Platform:ocispec.Platform{OS:"linux",Architecture:"amd64"}},
	}
	calls:=0
	resolver := func(_ context.Context, s nativeoci.Source)(nativeoci.ResolvedImage,error){
		calls++
		return nativeoci.ResolvedImage{
			Registry:s.Registry,Repository:s.Repository,
			ManifestDigest:"sha256:"+strings.Repeat("a",64),
			ConfigDigest:"sha256:"+strings.Repeat("b",64),
			IncusFingerprint:strings.Repeat("c",64),
		},nil
	}
	out,err:=resolveNativeGatewayImages(context.Background(),p,sources,resolver)
	if err!=nil {t.Fatal(err)}
	if calls!=2 {t.Fatalf("resolution calls = %d", calls)}
	if !strings.Contains(out.Topology.Services[0].ImageReference,"@sha256:") ||
		!strings.Contains(out.Topology.Services[1].ImageReference,"@sha256:") {t.Fatal("images not pinned")}
	if out.Postgres.ConfigDigest==out.Postgres.ManifestDigest {t.Fatal("config identity lost")}
	// Source intent must remain unchanged for audit/retry.
	if p.Services[0].ImageReference!="docker.io/library/postgres:16" {t.Fatal("input mutated")}
}

func TestResolveNativeGatewayImagesRejectsSourceMismatchWithoutNetwork(t *testing.T) {
	p:=nativePinnedFixture()
	sources:=NativeGatewayImageSources{
		Postgres:nativeoci.Source{Registry:"docker.io",Repository:"library/postgres",Reference:"16",Platform:ocispec.Platform{OS:"linux",Architecture:"amd64"}},
	}
	calls:=0
	_,err:=resolveNativeGatewayImages(context.Background(),p,sources,func(context.Context,nativeoci.Source)(nativeoci.ResolvedImage,error){
		calls++; return nativeoci.ResolvedImage{},nil
	})
	if err==nil || calls!=0 {t.Fatalf("expected preflight rejection before network, calls=%d err=%v",calls,err)}
}

func TestResolveNativeGatewayImagesCancellation(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background())
	cancel()
	_,err:=resolveNativeGatewayImages(ctx,nativePinnedFixture(),NativeGatewayImageSources{},nil)
	if !errors.Is(err,context.Canceled) {t.Fatalf("cancellation: %v",err)}
}
