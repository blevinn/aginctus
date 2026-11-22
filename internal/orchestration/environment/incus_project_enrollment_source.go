package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

const (
	incusEnvironmentEnrollmentKey = "user.aginctus.environments"
	incusEnvironmentEnrollmentVersion = "aginctus.io/environment-enrollment/v1alpha1"
	maxIncusEnvironmentEnrollmentBytes = 64 << 10
)

type incusEnvironmentEnrollmentManifest struct {
	APIVersion string       `json:"apiVersion"`
	Items      []Descriptor `json:"items"`
}

// IncusProjectEnrollmentSource reads an administrator-provisioned enrollment
// manifest from the exact Incus project's config, using the same restricted
// connection as the BindingVerifier. This is a read-only bootstrap/discovery
// record, NOT accepted-state storage and not a grant inferred from labels.
//
// Physical authorization is provided by the restricted Incus SDK connection,
// NOT by the JSON manifest. The manifest must be provisioned by the host's
// independently authorized project administration path. Its immutability
// against restricted project clients requires disposable VM verification.
type IncusProjectEnrollmentSource struct {
	Observer RestrictedIncusProjectObserver
}

// NewRestrictedIncusReadCatalog keeps the enrollment source and binding
// verifier on one connection. The caller still must independently configure
// that connection with a pinned server and project-restricted credentials;
// this constructor is not an authentication operation.
func NewRestrictedIncusReadCatalog(
	observer RestrictedIncusProjectObserver, trustedServerPin string,
) Catalog {
	return Catalog{
		Verifier: RestrictedIncusProjectVerifier{
			Observer: observer, PinnedServerCertificateSHA256: trustedServerPin,
		},
		Source: IncusProjectEnrollmentSource{Observer: observer},
	}
}

func (s IncusProjectEnrollmentSource) Enrolled(
	ctx context.Context, binding Binding,
) ([]Descriptor, error) {
	if ctx == nil {
		return nil, errors.New("project enrollment requires context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, err := incusscope.NewProjectScope(binding.Project)
	if err != nil || binding.Project == "default" ||
		strings.TrimSpace(binding.ServerID) == "" || s.Observer == nil {
		return nil, errors.New("verified project enrollment source required")
	}
	project, etag, err := s.Observer.GetProject(binding.Project)
	if err != nil || project == nil || project.Name != binding.Project ||
		etag == "" || project.Config == nil {
		return nil, errors.New("incus project enrollment unavailable")
	}
	raw, present := project.Config[incusEnvironmentEnrollmentKey]
	if !present || len(raw) == 0 || len(raw) > maxIncusEnvironmentEnrollmentBytes {
		return nil, errors.New("versioned project enrollment manifest unavailable")
	}
	if err := rejectCandidateDuplicateKeys([]byte(raw)); err != nil {
		return nil, errors.New("invalid Incus project enrollment JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil ||
		candidateKeys(fields, "apiVersion", "items") != nil {
		return nil, errors.New("invalid Incus project enrollment document")
	}
	var manifest incusEnvironmentEnrollmentManifest
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, errors.New("invalid Incus project enrollment fields")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("trailing Incus project enrollment data")
	}
	if manifest.APIVersion != incusEnvironmentEnrollmentVersion || manifest.Items == nil {
		return nil, errors.New("unsupported or incomplete Incus project enrollment")
	}
	out := make([]Descriptor, 0, len(manifest.Items))
	for _, d := range manifest.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if d.Validate() != nil || d.Binding != binding {
			return nil, errors.New("foreign or malformed Incus project enrollment")
		}
		out = append(out, d)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	again, againETag, err := s.Observer.GetProject(binding.Project)
	if err != nil || again == nil || again.Name != project.Name ||
		againETag != etag || again.Config[incusEnvironmentEnrollmentKey] != raw {
		return nil, errors.New("incus enrollment changed during read")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
