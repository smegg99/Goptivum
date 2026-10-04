// api/v1/requestcontext.go

package v1

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
)

// requestScoped hands every operation its request's own context.
//
// The generated gin strict handler passes the *gin.Context itself, and gin
// returns that to its pool for the next request once the response is written.
// database/sql and sqlite keep reading the context from goroutines of their
// own after the handler returns, so they would read whichever request has it
// by then. Each method is written out instead of embedding the interface:
// embedding would let a new operation compile and receive the pooled context.
type requestScoped struct {
	next gen.StrictServerInterface
}

// requestContext is the request's context behind a gin one.
func requestContext(ctx context.Context) context.Context {
	if c, ok := ctx.(*gin.Context); ok {
		return c.Request.Context()
	}
	return ctx
}

func (r requestScoped) ImportPlan(ctx context.Context, request gen.ImportPlanRequestObject) (gen.ImportPlanResponseObject, error) {
	return r.next.ImportPlan(requestContext(ctx), request)
}

func (r requestScoped) PreviewImport(ctx context.Context, request gen.PreviewImportRequestObject) (gen.PreviewImportResponseObject, error) {
	return r.next.PreviewImport(requestContext(ctx), request)
}

func (r requestScoped) CancelJob(ctx context.Context, request gen.CancelJobRequestObject) (gen.CancelJobResponseObject, error) {
	return r.next.CancelJob(requestContext(ctx), request)
}

func (r requestScoped) GetJob(ctx context.Context, request gen.GetJobRequestObject) (gen.GetJobResponseObject, error) {
	return r.next.GetJob(requestContext(ctx), request)
}

func (r requestScoped) ListNodes(ctx context.Context, request gen.ListNodesRequestObject) (gen.ListNodesResponseObject, error) {
	return r.next.ListNodes(requestContext(ctx), request)
}

func (r requestScoped) PairNode(ctx context.Context, request gen.PairNodeRequestObject) (gen.PairNodeResponseObject, error) {
	return r.next.PairNode(requestContext(ctx), request)
}

func (r requestScoped) RevokeNode(ctx context.Context, request gen.RevokeNodeRequestObject) (gen.RevokeNodeResponseObject, error) {
	return r.next.RevokeNode(requestContext(ctx), request)
}

func (r requestScoped) GetNode(ctx context.Context, request gen.GetNodeRequestObject) (gen.GetNodeResponseObject, error) {
	return r.next.GetNode(requestContext(ctx), request)
}

func (r requestScoped) UpdateNode(ctx context.Context, request gen.UpdateNodeRequestObject) (gen.UpdateNodeResponseObject, error) {
	return r.next.UpdateNode(requestContext(ctx), request)
}

func (r requestScoped) GetNodeStatus(ctx context.Context, request gen.GetNodeStatusRequestObject) (gen.GetNodeStatusResponseObject, error) {
	return r.next.GetNodeStatus(requestContext(ctx), request)
}

func (r requestScoped) ListNodeJobs(ctx context.Context, request gen.ListNodeJobsRequestObject) (gen.ListNodeJobsResponseObject, error) {
	return r.next.ListNodeJobs(requestContext(ctx), request)
}

func (r requestScoped) ListPlans(ctx context.Context, request gen.ListPlansRequestObject) (gen.ListPlansResponseObject, error) {
	return r.next.ListPlans(requestContext(ctx), request)
}

func (r requestScoped) CreatePlan(ctx context.Context, request gen.CreatePlanRequestObject) (gen.CreatePlanResponseObject, error) {
	return r.next.CreatePlan(requestContext(ctx), request)
}

func (r requestScoped) CreateDemoPlan(ctx context.Context, request gen.CreateDemoPlanRequestObject) (gen.CreateDemoPlanResponseObject, error) {
	return r.next.CreateDemoPlan(requestContext(ctx), request)
}

func (r requestScoped) DeletePlan(ctx context.Context, request gen.DeletePlanRequestObject) (gen.DeletePlanResponseObject, error) {
	return r.next.DeletePlan(requestContext(ctx), request)
}

func (r requestScoped) GetPlan(ctx context.Context, request gen.GetPlanRequestObject) (gen.GetPlanResponseObject, error) {
	return r.next.GetPlan(requestContext(ctx), request)
}

func (r requestScoped) UpdatePlan(ctx context.Context, request gen.UpdatePlanRequestObject) (gen.UpdatePlanResponseObject, error) {
	return r.next.UpdatePlan(requestContext(ctx), request)
}

func (r requestScoped) ListJobs(ctx context.Context, request gen.ListJobsRequestObject) (gen.ListJobsResponseObject, error) {
	return r.next.ListJobs(requestContext(ctx), request)
}

func (r requestScoped) CreateJob(ctx context.Context, request gen.CreateJobRequestObject) (gen.CreateJobResponseObject, error) {
	return r.next.CreateJob(requestContext(ctx), request)
}

func (r requestScoped) RestorePlan(ctx context.Context, request gen.RestorePlanRequestObject) (gen.RestorePlanResponseObject, error) {
	return r.next.RestorePlan(requestContext(ctx), request)
}

func (r requestScoped) ListVersions(ctx context.Context, request gen.ListVersionsRequestObject) (gen.ListVersionsResponseObject, error) {
	return r.next.ListVersions(requestContext(ctx), request)
}

func (r requestScoped) UploadVersion(ctx context.Context, request gen.UploadVersionRequestObject) (gen.UploadVersionResponseObject, error) {
	return r.next.UploadVersion(requestContext(ctx), request)
}

func (r requestScoped) DownloadVersion(ctx context.Context, request gen.DownloadVersionRequestObject) (gen.DownloadVersionResponseObject, error) {
	return r.next.DownloadVersion(requestContext(ctx), request)
}

func (r requestScoped) DeleteCheckpoint(ctx context.Context, request gen.DeleteCheckpointRequestObject) (gen.DeleteCheckpointResponseObject, error) {
	return r.next.DeleteCheckpoint(requestContext(ctx), request)
}

func (r requestScoped) NameCheckpoint(ctx context.Context, request gen.NameCheckpointRequestObject) (gen.NameCheckpointResponseObject, error) {
	return r.next.NameCheckpoint(requestContext(ctx), request)
}

func (r requestScoped) RestoreCheckpoint(ctx context.Context, request gen.RestoreCheckpointRequestObject) (gen.RestoreCheckpointResponseObject, error) {
	return r.next.RestoreCheckpoint(requestContext(ctx), request)
}

func (r requestScoped) GetSchoolKey(ctx context.Context, request gen.GetSchoolKeyRequestObject) (gen.GetSchoolKeyResponseObject, error) {
	return r.next.GetSchoolKey(requestContext(ctx), request)
}

func (r requestScoped) ReleaseSchoolKey(ctx context.Context, request gen.ReleaseSchoolKeyRequestObject) (gen.ReleaseSchoolKeyResponseObject, error) {
	return r.next.ReleaseSchoolKey(requestContext(ctx), request)
}

func (r requestScoped) ListTrash(ctx context.Context, request gen.ListTrashRequestObject) (gen.ListTrashResponseObject, error) {
	return r.next.ListTrash(requestContext(ctx), request)
}

func (r requestScoped) ListChanges(ctx context.Context, request gen.ListChangesRequestObject) (gen.ListChangesResponseObject, error) {
	return r.next.ListChanges(requestContext(ctx), request)
}

func (r requestScoped) ListUsers(ctx context.Context, request gen.ListUsersRequestObject) (gen.ListUsersResponseObject, error) {
	return r.next.ListUsers(requestContext(ctx), request)
}

func (r requestScoped) CreateUser(ctx context.Context, request gen.CreateUserRequestObject) (gen.CreateUserResponseObject, error) {
	return r.next.CreateUser(requestContext(ctx), request)
}

func (r requestScoped) GetUser(ctx context.Context, request gen.GetUserRequestObject) (gen.GetUserResponseObject, error) {
	return r.next.GetUser(requestContext(ctx), request)
}

func (r requestScoped) UpdateUser(ctx context.Context, request gen.UpdateUserRequestObject) (gen.UpdateUserResponseObject, error) {
	return r.next.UpdateUser(requestContext(ctx), request)
}

func (r requestScoped) ResetUserPassword(ctx context.Context, request gen.ResetUserPasswordRequestObject) (gen.ResetUserPasswordResponseObject, error) {
	return r.next.ResetUserPassword(requestContext(ctx), request)
}

func (r requestScoped) CheckVersion(ctx context.Context, request gen.CheckVersionRequestObject) (gen.CheckVersionResponseObject, error) {
	return r.next.CheckVersion(requestContext(ctx), request)
}

func (r requestScoped) GetRuleCatalogue(ctx context.Context, request gen.GetRuleCatalogueRequestObject) (gen.GetRuleCatalogueResponseObject, error) {
	return r.next.GetRuleCatalogue(requestContext(ctx), request)
}

func (r requestScoped) ListAllJobs(ctx context.Context, request gen.ListAllJobsRequestObject) (gen.ListAllJobsResponseObject, error) {
	return r.next.ListAllJobs(requestContext(ctx), request)
}

func (r requestScoped) GetJobPreview(ctx context.Context, request gen.GetJobPreviewRequestObject) (gen.GetJobPreviewResponseObject, error) {
	return r.next.GetJobPreview(requestContext(ctx), request)
}

func (r requestScoped) ListResults(ctx context.Context, request gen.ListResultsRequestObject) (gen.ListResultsResponseObject, error) {
	return r.next.ListResults(requestContext(ctx), request)
}

func (r requestScoped) UploadResult(ctx context.Context, request gen.UploadResultRequestObject) (gen.UploadResultResponseObject, error) {
	return r.next.UploadResult(requestContext(ctx), request)
}

func (r requestScoped) DownloadResult(ctx context.Context, request gen.DownloadResultRequestObject) (gen.DownloadResultResponseObject, error) {
	return r.next.DownloadResult(requestContext(ctx), request)
}

func (r requestScoped) UpdateResult(ctx context.Context, request gen.UpdateResultRequestObject) (gen.UpdateResultResponseObject, error) {
	return r.next.UpdateResult(requestContext(ctx), request)
}

func (r requestScoped) DeleteResult(ctx context.Context, request gen.DeleteResultRequestObject) (gen.DeleteResultResponseObject, error) {
	return r.next.DeleteResult(requestContext(ctx), request)
}

func (r requestScoped) RestoreResult(ctx context.Context, request gen.RestoreResultRequestObject) (gen.RestoreResultResponseObject, error) {
	return r.next.RestoreResult(requestContext(ctx), request)
}

func (r requestScoped) CheckResult(ctx context.Context, request gen.CheckResultRequestObject) (gen.CheckResultResponseObject, error) {
	return r.next.CheckResult(requestContext(ctx), request)
}

func (r requestScoped) ExportVersion(ctx context.Context, request gen.ExportVersionRequestObject) (gen.ExportVersionResponseObject, error) {
	return r.next.ExportVersion(requestContext(ctx), request)
}

func (r requestScoped) ExportResult(ctx context.Context, request gen.ExportResultRequestObject) (gen.ExportResultResponseObject, error) {
	return r.next.ExportResult(requestContext(ctx), request)
}

func (r requestScoped) GetAccountLibrary(ctx context.Context, request gen.GetAccountLibraryRequestObject) (gen.GetAccountLibraryResponseObject, error) {
	return r.next.GetAccountLibrary(requestContext(ctx), request)
}

func (r requestScoped) SaveAccountLibrary(ctx context.Context, request gen.SaveAccountLibraryRequestObject) (gen.SaveAccountLibraryResponseObject, error) {
	return r.next.SaveAccountLibrary(requestContext(ctx), request)
}

func (r requestScoped) ListRuleProfiles(ctx context.Context, request gen.ListRuleProfilesRequestObject) (gen.ListRuleProfilesResponseObject, error) {
	return r.next.ListRuleProfiles(requestContext(ctx), request)
}

func (r requestScoped) CreateRuleProfile(ctx context.Context, request gen.CreateRuleProfileRequestObject) (gen.CreateRuleProfileResponseObject, error) {
	return r.next.CreateRuleProfile(requestContext(ctx), request)
}

func (r requestScoped) UpdateRuleProfile(ctx context.Context, request gen.UpdateRuleProfileRequestObject) (gen.UpdateRuleProfileResponseObject, error) {
	return r.next.UpdateRuleProfile(requestContext(ctx), request)
}

func (r requestScoped) DeleteRuleProfile(ctx context.Context, request gen.DeleteRuleProfileRequestObject) (gen.DeleteRuleProfileResponseObject, error) {
	return r.next.DeleteRuleProfile(requestContext(ctx), request)
}

func (r requestScoped) ListUserSessions(ctx context.Context, request gen.ListUserSessionsRequestObject) (gen.ListUserSessionsResponseObject, error) {
	return r.next.ListUserSessions(requestContext(ctx), request)
}

func (r requestScoped) RevokeUserSessions(ctx context.Context, request gen.RevokeUserSessionsRequestObject) (gen.RevokeUserSessionsResponseObject, error) {
	return r.next.RevokeUserSessions(requestContext(ctx), request)
}

func (r requestScoped) RevokeUserSession(ctx context.Context, request gen.RevokeUserSessionRequestObject) (gen.RevokeUserSessionResponseObject, error) {
	return r.next.RevokeUserSession(requestContext(ctx), request)
}

func (r requestScoped) GetLockout(ctx context.Context, request gen.GetLockoutRequestObject) (gen.GetLockoutResponseObject, error) {
	return r.next.GetLockout(requestContext(ctx), request)
}

func (r requestScoped) UpdateLockout(ctx context.Context, request gen.UpdateLockoutRequestObject) (gen.UpdateLockoutResponseObject, error) {
	return r.next.UpdateLockout(requestContext(ctx), request)
}

func (r requestScoped) GetPasswordRules(ctx context.Context, request gen.GetPasswordRulesRequestObject) (gen.GetPasswordRulesResponseObject, error) {
	return r.next.GetPasswordRules(requestContext(ctx), request)
}

func (r requestScoped) GetServerSettings(ctx context.Context, request gen.GetServerSettingsRequestObject) (gen.GetServerSettingsResponseObject, error) {
	return r.next.GetServerSettings(requestContext(ctx), request)
}

func (r requestScoped) UpdateServerSettings(ctx context.Context, request gen.UpdateServerSettingsRequestObject) (gen.UpdateServerSettingsResponseObject, error) {
	return r.next.UpdateServerSettings(requestContext(ctx), request)
}

func (r requestScoped) GetStorage(ctx context.Context, request gen.GetStorageRequestObject) (gen.GetStorageResponseObject, error) {
	return r.next.GetStorage(requestContext(ctx), request)
}

func (r requestScoped) EmptyTrash(ctx context.Context, request gen.EmptyTrashRequestObject) (gen.EmptyTrashResponseObject, error) {
	return r.next.EmptyTrash(requestContext(ctx), request)
}

func (r requestScoped) TrimBackups(ctx context.Context, request gen.TrimBackupsRequestObject) (gen.TrimBackupsResponseObject, error) {
	return r.next.TrimBackups(requestContext(ctx), request)
}

func (r requestScoped) SweepObjects(ctx context.Context, request gen.SweepObjectsRequestObject) (gen.SweepObjectsResponseObject, error) {
	return r.next.SweepObjects(requestContext(ctx), request)
}

func (r requestScoped) ListLogs(ctx context.Context, request gen.ListLogsRequestObject) (gen.ListLogsResponseObject, error) {
	return r.next.ListLogs(requestContext(ctx), request)
}

func (r requestScoped) ListAudit(ctx context.Context, request gen.ListAuditRequestObject) (gen.ListAuditResponseObject, error) {
	return r.next.ListAudit(requestContext(ctx), request)
}

func (r requestScoped) ForkPlan(ctx context.Context, request gen.ForkPlanRequestObject) (gen.ForkPlanResponseObject, error) {
	return r.next.ForkPlan(requestContext(ctx), request)
}

func (r requestScoped) TransferPlan(ctx context.Context, request gen.TransferPlanRequestObject) (gen.TransferPlanResponseObject, error) {
	return r.next.TransferPlan(requestContext(ctx), request)
}
