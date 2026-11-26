package environment

import (
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strings"

	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// GatewaySpec is accepted intent for the environment-owned PostgreSQL/LiteLLM
// pair. Network and pool references select existing, independently approved
// resources; they never request host provisioning. The credential version is
// a reference to environment-scoped delivery, not a value, path, or grant.
type GatewaySpec struct {
	ManagementNetwork string           `json:"managementNetwork"`
	EgressNetwork     string           `json:"egressNetwork"`
	EgressIPv4        string           `json:"egressIPv4"`
	RootPool          string           `json:"rootPool"`
	DatabasePool      string           `json:"databasePool"`
	Postgres          GatewayImageSpec `json:"postgres"`
	LiteLLM           GatewayImageSpec `json:"litellm"`
	CredentialVersion string           `json:"credentialVersion"`
	EnsureRunning     bool             `json:"ensureRunning"`
}

type GatewayImageSpec struct {
	Registry         string `json:"registry"`
	Repository       string `json:"repository"`
	Architecture     string `json:"architecture"`
	Variant          string `json:"variant"`
	ManifestDigest   string `json:"manifestDigest"`
	ConfigDigest     string `json:"configDigest"`
	IncusFingerprint string `json:"incusFingerprint"`
	Alias            string `json:"alias"`
}

const (
	gatewayNamePrefix     = "ag-"
	gatewayPostgresSuffix = "-pg"
	gatewayLiteLLMSuffix  = "-llm"
)

// GatewayResourceNames is the physical naming rule for a validated environment
// ID. These names neither establish ownership nor grant authority.
func GatewayResourceNames(id string) (postgres, liteLLM, database string) {
	base := gatewayNamePrefix + strings.ReplaceAll(id, "-", "")
	return base + gatewayPostgresSuffix, base + gatewayLiteLLMSuffix, base + "-pgdata"
}

func isGatewayInstanceName(name string) bool {
	compact, ok := strings.CutPrefix(name, gatewayNamePrefix)
	if !ok {
		return false
	}
	if trimmed, pg := strings.CutSuffix(compact, gatewayPostgresSuffix); pg {
		compact = trimmed
	} else if trimmed, llm := strings.CutSuffix(compact, gatewayLiteLLMSuffix); llm {
		compact = trimmed
	} else {
		return false
	}
	if len(compact) != 32 {
		return false
	}
	return idPattern.MatchString(compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:])
}

func (i GatewayImageSpec) Source() nativeoci.Source {
	return nativeoci.Source{Registry: i.Registry, Repository: i.Repository, Reference: i.ManifestDigest,
		Platform: ocispec.Platform{OS: "linux", Architecture: i.Architecture, Variant: i.Variant}}
}

var gatewayReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)
var gatewayPlatformPart = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
var gatewayCredentialVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func (g GatewaySpec) validate(binding Binding) error {
	if binding.Project == "default" || !gatewayReference.MatchString(g.ManagementNetwork) ||
		!gatewayReference.MatchString(g.RootPool) || !gatewayReference.MatchString(g.DatabasePool) ||
		!gatewayCredentialVersion.MatchString(g.CredentialVersion) || g.Postgres.Alias == g.LiteLLM.Alias {
		return errors.New("invalid gateway scope or references")
	}
	if g.EgressNetwork == "" {
		if g.EgressIPv4 != "" {
			return errors.New("gateway egress address requires a network")
		}
	} else {
		p, err := netip.ParsePrefix(g.EgressIPv4)
		if !gatewayReference.MatchString(g.EgressNetwork) || g.EgressNetwork == g.ManagementNetwork ||
			err != nil || !p.Addr().Is4() || p.Bits() < 16 || p.Bits() > 30 ||
			!p.Addr().IsGlobalUnicast() || p.String() != g.EgressIPv4 {
			return errors.New("invalid gateway egress reference")
		}
		addr := p.Addr().As4()
		mask := uint32(1)<<(32-p.Bits()) - 1
		host := (uint32(addr[2])<<8 | uint32(addr[3])) & mask
		if host == 0 || host == mask {
			return errors.New("gateway egress address must name a host")
		}
	}
	for _, image := range []GatewayImageSpec{g.Postgres, g.LiteLLM} {
		if len(image.Registry) > 253 || len(image.Repository) > 255 || image.Source().Validate() != nil ||
			!gatewayPlatformPart.MatchString(image.Architecture) || (image.Variant != "" && !gatewayPlatformPart.MatchString(image.Variant)) ||
			!gatewayReference.MatchString(image.Alias) || !validAcceptedHash(image.IncusFingerprint) ||
			!validGatewayDigest(image.ManifestDigest) || !validGatewayDigest(image.ConfigDigest) {
			return errors.New("invalid immutable gateway image identity")
		}
	}
	return nil
}

func validGatewayDigest(s string) bool {
	hash, ok := strings.CutPrefix(s, "sha256:")
	return ok && validAcceptedHash(hash)
}

func gatewayCandidateKeys(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || candidateKeys(fields, "managementNetwork", "egressNetwork", "egressIPv4", "rootPool",
		"databasePool", "postgres", "litellm", "credentialVersion", "ensureRunning") != nil {
		return errors.New("invalid gateway candidate fields")
	}
	for _, role := range []string{"postgres", "litellm"} {
		var image map[string]json.RawMessage
		if json.Unmarshal(fields[role], &image) != nil || candidateKeys(image, "registry", "repository", "architecture",
			"variant", "manifestDigest", "configDigest", "incusFingerprint", "alias") != nil {
			return errors.New("invalid gateway image fields")
		}
	}
	return nil
}

func sameGatewaySpec(a, b *GatewaySpec) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
