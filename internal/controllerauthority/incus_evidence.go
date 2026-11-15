package controllerauthority

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/blevinn/aginctus/internal/acceptedstate"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// DaemonTLSObservation is read-only evidence returned by a restricted TLS
// session, NOT a credential, a controller lease, or permission to write.
// The reader must be bound to the same actual remote/certificate for every
// call; never feed in an untrusted CLI's self-reported identity.
type DaemonTLSObservation struct {
	Project               string
	CertificateFingerprint string
	VerifiedRestriction   bool
}

// ReadIncusMetadata MUST issue a GET using the same TLS session/credential
// and return only the successful HTTP 200 "metadata" object. It must return
// an error for 401/403/404/transport changes; do not coerce denial or
// filtered empty lists into an authorized result.
type ReadIncusMetadata func(context.Context, string) (json.RawMessage, error)

// ObserveRestrictedOperator performs read-only, live server-side checks for
// one exact Incus project and explicitly authorized host bridge/storage pool.
// It does not infer trust from a CLI remote name or `--project` flag.
//
// Security note: callback provenance cannot be proved by this pure function.
// A future broker/executor must own the TLS transport itself and authenticate
// the peer UID independently via kernel credentials before using these
// observations as inputs to its mutation-time checks.
func ObserveRestrictedOperator(ctx context.Context, project, bridge, pool string, read ReadIncusMetadata) (DaemonTLSObservation, error) {
	if ctx == nil || read == nil {
		return DaemonTLSObservation{}, errors.New("restricted identity observer requires context and a bound metadata reader")
	}
	scope, err := incusscope.NewProjectScope(project)
	if err != nil {
		return DaemonTLSObservation{}, errors.New("restricted identity observer requires an explicit nondefault project")
	}
	required, err := scope.ProposedRestrictedOperatorConfig(bridge, pool)
	if err != nil {
		return DaemonTLSObservation{}, errors.New("restricted identity observer requires explicit approved host capability names")
	}
	// The executed #756 VM requires managed NIC and disk devices. Until
	// every supported host is reviewed, do not weaken this disk restriction,
	// even if a caller gives a policy candidate from an older release.
	required["restricted.devices.disk"] = "managed"

	get := func(path string, output any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := read(ctx, path)
		if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
			return errors.New("Incus identity evidence unavailable")
		}
		if err := json.Unmarshal(raw, output); err != nil {
			return errors.New("invalid Incus identity evidence")
		}
		return ctx.Err()
	}
	var server struct {
		Auth string `json:"auth"`
		Method string `json:"auth_user_method"`
		Fingerprint string `json:"auth_user_name"`
	}
	if err := get("/1.0?project="+url.QueryEscape(project), &server); err != nil {
		return DaemonTLSObservation{}, err
	}
	if server.Auth != "trusted" || server.Method != "tls" || !hashPattern.MatchString(server.Fingerprint) {
		return DaemonTLSObservation{}, errors.New("restricted TLS principal not established by Incus daemon")
	}
	var cert struct {
		Fingerprint string `json:"fingerprint"`
		Restricted *bool `json:"restricted"`
		Projects []string `json:"projects"`
	}
	if err := get("/1.0/certificates/"+server.Fingerprint, &cert); err != nil {
		return DaemonTLSObservation{}, err
	}
	if cert.Fingerprint != server.Fingerprint || cert.Restricted == nil || !*cert.Restricted ||
		len(cert.Projects) != 1 || cert.Projects[0] != project {
		return DaemonTLSObservation{}, errors.New("Incus certificate is not confined to the exact operator project")
	}
	var observed struct {
		Name   string            `json:"name"`
		Config map[string]string `json:"config"`
	}
	if err := get("/1.0/projects/"+url.PathEscape(project), &observed); err != nil {
		return DaemonTLSObservation{}, err
	}
	if observed.Name != project || observed.Config == nil {
		return DaemonTLSObservation{}, errors.New("Incus project identity or policy unavailable")
	}
	for key, expected := range required {
		if observed.Config[key] != expected {
			return DaemonTLSObservation{}, fmt.Errorf("restricted project policy mismatch at %s", key)
		}
	}
	// Unknown additional restricted/feature settings can broaden the grant,
	// or change inheritance semantics. Do not infer their safety.
	for key := range observed.Config {
		if strings.HasPrefix(key, "restricted.") || strings.HasPrefix(key, "features.") {
			if _, permitted := required[key]; !permitted {
				return DaemonTLSObservation{}, errors.New("unrecognized Incus project restriction or feature")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return DaemonTLSObservation{}, err
	}
	return DaemonTLSObservation{
		Project: project, CertificateFingerprint: server.Fingerprint,
		VerifiedRestriction: true,
	}, nil
}

// AssessAcceptedOperatorSelection joins a trusted host enrollment snapshot,
// an accepted pinned workload, and read-only TLS server observations. It
// NEVER yields a write authorization or controller fence.
//
// A caller cannot assert a matching certificate by supplying arbitrary
// `DaemonTLSObservation`: only a future server-bound transport can prove
// this provenance. Revalidate on a fresh session immediately before writes.
func (h *HostEvidence) AssessAcceptedOperatorSelection(
	pin acceptedstate.PinnedWorkloadSnapshot, uid uint32, tls DaemonTLSObservation, action string,
) Assessment {
	if !tls.VerifiedRestriction || tls.Project != pin.IncusProject ||
		!hashPattern.MatchString(tls.CertificateFingerprint) ||
		pin.Workload.ID == "" {
		return Assessment{Code: EnrollmentUntrusted}
	}
	return h.Assess(AssessedPrincipal{
		UID: uid, TLSFingerprint: tls.CertificateFingerprint, ProjectRestricted: true,
	}, SelectedRevision{
		DeploymentID: pin.DeploymentID, Project: pin.IncusProject,
		WorkloadID: pin.Workload.ID, RevisionID: pin.Revision.ID,
		Generation: pin.Revision.Generation,
	}, action)
}
