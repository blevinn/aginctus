package environment

import "context"

// RecoverProjectOperation requests an exact, current-revision recovery under a
// separately administered recover grant. It submits no Incus effects.
func RecoverProjectOperation(ctx context.Context, config RestrictedOperatorReadConfig, project, environmentID, revisionID, workloadID, operationID string) error {
	if !validAcceptedHash(revisionID) || !idPattern.MatchString(environmentID) ||
		!journalIdentifier.MatchString(workloadID) || !localEnvironmentAttemptID.MatchString(operationID) {
		return errProjectExecutionDenied
	}
	reads, closeReads, err := openProductionOperatorReads(ctx, config)
	if err != nil {
		return err
	}
	defer closeReads()
	if reads.stateSession == nil {
		return errProjectExecutionDenied
	}
	desc, err := reads.AcceptedSnapshot.Catalog.Get(ctx, project, environmentID)
	if err != nil {
		return errProjectExecutionDenied
	}
	session, err := reads.stateSession.open(ctx, desc)
	if err != nil {
		return err
	}
	rev, candidate, err := session.ReadPublished(ctx, desc.ID)
	if err != nil || rev == nil || candidate == nil || rev.ID != revisionID {
		return ErrAcceptedConflict
	}
	_, err = sendProjectExecution(ctx, session, projectExecutionRequest{
		APIVersion: projectExecutionWireVersion, Step: "recover", Operation: "recover",
		ID: operationID, WorkloadID: workloadID, Revision: *rev,
	})
	return err
}
