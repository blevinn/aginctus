package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
)

// NativeGatewayImageSources are explicit platform-specific public OCI sources.
// They are not inferred from ambient Docker or Incus credentials.
type NativeGatewayImageSources struct {
	Postgres nativeoci.Source
	LiteLLM nativeoci.Source
}

// NativeResolvedGateway retains the registry manifest/config identities in
// addition to the graph's digest-pinned acquisition sources. Incus's layer-only
// fingerprint is not a substitute for the registry config digest.
type NativeResolvedGateway struct {
	Topology NativeGatewayTopology
	Postgres nativeoci.ResolvedImage
	LiteLLM nativeoci.ResolvedImage
}

type nativeGatewayImageResolver func(context.Context, nativeoci.Source) (nativeoci.ResolvedImage, error)

// ResolveNativeGatewayImages is metadata-only. It deliberately does not
// import images, reuse daemon fingerprints, create aliases, or grant write
// authority. A future executor must separately validate resource ownership,
// image identity collisions and the accepted revision.
func ResolveNativeGatewayImages(ctx context.Context, topology NativeGatewayTopology, sources NativeGatewayImageSources) (NativeResolvedGateway, error) {
	return resolveNativeGatewayImages(ctx, topology, sources, nativeoci.ResolvePublic)
}

func resolveNativeGatewayImages(ctx context.Context, topology NativeGatewayTopology, sources NativeGatewayImageSources, resolve nativeGatewayImageResolver) (NativeResolvedGateway, error) {
	if err := ctx.Err(); err != nil { return NativeResolvedGateway{}, err }
	if len(topology.Services) != 2 || topology.Services[0].Name != "postgres" || topology.Services[1].Name != "litellm" {
		return NativeResolvedGateway{}, fmt.Errorf("native gateway image resolution requires exact service roles")
	}
	if resolve == nil { return NativeResolvedGateway{}, fmt.Errorf("native gateway image resolver is required") }
	out := NativeResolvedGateway{Topology: topology}
	for i, entry := range []struct {
		source nativeoci.Source
		result *nativeoci.ResolvedImage
	}{{sources.Postgres, &out.Postgres}, {sources.LiteLLM, &out.LiteLLM}} {
		if err := entry.source.Validate(); err != nil {
			return NativeResolvedGateway{}, fmt.Errorf("native gateway OCI source %d rejected", i)
		}
		marker := ":"
		if strings.HasPrefix(entry.source.Reference, "sha256:") { marker = "@" }
		want := entry.source.Registry + "/" + entry.source.Repository + marker + entry.source.Reference
		if topology.Services[i].ImageReference != want {
			return NativeResolvedGateway{}, fmt.Errorf("native gateway OCI source %d does not match configured image", i)
		}
		resolved, err := resolve(ctx, entry.source)
		if err != nil {
			if ctx.Err() != nil { return NativeResolvedGateway{}, ctx.Err() }
			return NativeResolvedGateway{}, fmt.Errorf("native gateway OCI identity resolution failed for service %d", i)
		}
		if resolved.Registry != entry.source.Registry || resolved.Repository != entry.source.Repository ||
			!strings.HasPrefix(resolved.ManifestDigest, "sha256:") || len(resolved.ManifestDigest) != 71 ||
			!strings.HasPrefix(resolved.ConfigDigest, "sha256:") || len(resolved.ConfigDigest) != 71 ||
			len(resolved.IncusFingerprint) != 64 {
			return NativeResolvedGateway{}, fmt.Errorf("native gateway OCI resolver returned incomplete identity for service %d", i)
		}
		*entry.result = resolved
		out.Topology.Services[i].ImageReference = resolved.Registry + "/" + resolved.Repository + "@" + resolved.ManifestDigest
	}
	if err := out.Topology.ValidateExecutionShape(); err != nil {
		return NativeResolvedGateway{}, err
	}
	return out, nil
}
