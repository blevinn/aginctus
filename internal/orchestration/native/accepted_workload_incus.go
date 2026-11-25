package native

import (
	"context"
	"errors"
	"net/http"
	"slices"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

// AcceptedWorkloadIncusReader is the read-only SDK surface used to verify
// scope and read named instances on the very same connection.
type AcceptedWorkloadIncusReader interface {
	environment.RestrictedIncusProjectObserver
	GetInstance(string) (*api.Instance, string, error)
}

var _ AcceptedWorkloadIncusReader = (incusclient.InstanceServer)(nil)

// ObservePinnedAcceptedWorkloads compares one already-pinned accepted revision
// with named Incus reads. The caller must obtain the enrolled descriptor and
// pinned revision/body from authenticated project state and supply an exclusive,
// project-scoped SDK connection with independently pinned TLS (for example via
// environment.OpenExplicitRestrictedIncusProject, using ctx). trustedServerPin
// is the independently provisioned SHA-256 hex, never derived from the candidate.
// The connection must use standard Incus semantics (permission denied is not
// hidden as instance not found). A caller-supplied implementation is not itself
// proof of authentication. Keep any caller-owned pin held through the call.
//
// Only the named selected instances are read; the verifier's default-project
// denial probe is not a workload read or a fallback. The result is a comparison
// of sequential reads, not an atomic runtime snapshot, readiness, or a write
// fence. No accepted-state input is rediscovered or refreshed here.
func ObservePinnedAcceptedWorkloads(
	ctx context.Context, enrolled environment.Descriptor, revision environment.AcceptedRevision,
	accepted environment.Candidate, reader AcceptedWorkloadIncusReader, trustedServerPin string,
) (AcceptedEnvironmentAssessment, error) {
	if ctx == nil {
		return AcceptedEnvironmentAssessment{}, errors.New("accepted workload observation requires context")
	}
	if err := ctx.Err(); err != nil {
		return AcceptedEnvironmentAssessment{}, err
	}
	if reader == nil || enrolled.Validate() != nil || enrolled.Binding.Project == api.ProjectDefaultName ||
		!fingerprintPattern.MatchString(trustedServerPin) || enrolled.Binding.ServerID != "sha256:"+trustedServerPin ||
		!accepted.Environment.SameEnvironment(enrolled) || revision.ValidateAgainst(accepted) != nil {
		return AcceptedEnvironmentAssessment{}, errors.New("pinned accepted revision and independently pinned Incus project required")
	}
	// Detach the verified selection before invoking any reader. A subsequent
	// caller-side edit must not redirect later reads to names outside the pin.
	accepted.SelectedWorkloads = slices.Clone(accepted.SelectedWorkloads)
	accepted.Workloads = slices.Clone(accepted.Workloads)
	for i := range accepted.Workloads {
		accepted.Workloads[i].Profiles = slices.Clone(accepted.Workloads[i].Profiles)
	}

	connectionScope := func() (acceptedIncusScope, error) {
		if err := ctx.Err(); err != nil {
			return acceptedIncusScope{}, err
		}
		info, err := reader.GetConnectionInfo()
		if err != nil || info == nil {
			return acceptedIncusScope{}, observationFailure(ctx, "accepted workload Incus scope unavailable")
		}
		return acceptedIncusScope{info.Protocol, info.SocketPath, info.URL, info.Certificate, info.Project, info.Target}, ctx.Err()
	}
	scope, err := connectionScope()
	if err != nil {
		return AcceptedEnvironmentAssessment{}, err
	}
	checkScope := func() error {
		current, err := connectionScope()
		if err != nil || current != scope {
			return observationFailure(ctx, "accepted workload Incus scope changed during observation")
		}
		return nil
	}
	verifier := environment.RestrictedIncusProjectVerifier{Observer: reader, PinnedServerCertificateSHA256: trustedServerPin}
	verify := func() error {
		binding, err := verifier.VerifyProject(ctx, enrolled.Binding.Project)
		if err != nil || enrolled.VerifyBinding(binding) != nil {
			return observationFailure(ctx, "accepted workload Incus project verification failed")
		}
		return checkScope()
	}
	if err := verify(); err != nil {
		return AcceptedEnvironmentAssessment{}, err
	}

	observations := make([]AcceptedWorkloadObservation, 0, len(accepted.Workloads))
	for _, workload := range accepted.Workloads {
		if err := ctx.Err(); err != nil {
			return AcceptedEnvironmentAssessment{}, err
		}
		instance, etag, err := reader.GetInstance(workload.InstanceName)
		if scopeErr := checkScope(); scopeErr != nil {
			return AcceptedEnvironmentAssessment{}, scopeErr
		}
		item := AcceptedWorkloadObservation{WorkloadID: workload.ID}
		switch {
		case instance == nil && etag == "" && api.StatusErrorCheck(err, http.StatusNotFound):
			item.Absent = true
		case err != nil || instance == nil || etag == "":
			return AcceptedEnvironmentAssessment{}, observationFailure(ctx, "selected accepted workload observation unavailable")
		case instance.Project != enrolled.Binding.Project || instance.Name != workload.InstanceName:
			return AcceptedEnvironmentAssessment{}, errors.New("selected accepted workload identity differs from Incus response")
		case instance.Type != string(api.InstanceTypeContainer) && instance.Type != string(api.InstanceTypeVM):
			return AcceptedEnvironmentAssessment{}, errors.New("selected accepted workload instance type unverified")
		default:
			observed := instanceObservation(instance, instance.Config["volatile.base_image"])
			item.Instance = &observed
		}
		observations = append(observations, item)
	}
	if err := verify(); err != nil {
		return AcceptedEnvironmentAssessment{}, err
	}
	assessment, err := AssessPinnedAcceptedWorkloads(enrolled, revision, accepted, observations)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return AcceptedEnvironmentAssessment{}, ctxErr
	}
	return assessment, err
}

// Compare stable request routing metadata across the whole observation, not
// SDK-discovered addresses, which are not used to select another endpoint.
type acceptedIncusScope struct {
	protocol, socket, url, certificate, project, target string
}
