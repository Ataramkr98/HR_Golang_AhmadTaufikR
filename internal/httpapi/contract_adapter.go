package httpapi

import (
	"github.com/gin-gonic/gin"
	generated "github.com/simpul/hr-backend/internal/api/generated"
)

// ContractAdapter is the compile-time bridge between the generated OpenAPI
// interface and the domain handlers. Routing still attaches endpoint-specific
// RBAC middleware in server.go and the module route registrars.
type ContractAdapter struct{ server *Server }

var _ generated.ServerInterface = (*ContractAdapter)(nil)

func bindParam(c *gin.Context, key, value string) {
	c.Params = append(c.Params, gin.Param{Key: key, Value: value})
}

func (a *ContractAdapter) WorkforceAnalytics(c *gin.Context) { a.server.workforceAnalytics(c) }
func (a *ContractAdapter) ListAttendance(c *gin.Context)     { a.server.listAttendance(c) }
func (a *ContractAdapter) ExportAttendance(c *gin.Context, _ generated.ExportAttendanceParams) {
	a.server.exportAttendance(c)
}
func (a *ContractAdapter) CreateManualAttendance(c *gin.Context) { a.server.manualAttendance(c) }
func (a *ContractAdapter) ClockIn(c *gin.Context)                { a.server.clockIn(c) }
func (a *ContractAdapter) ClockOut(c *gin.Context)               { a.server.clockOut(c) }
func (a *ContractAdapter) Login(c *gin.Context)                  { a.server.login(c) }
func (a *ContractAdapter) Logout(c *gin.Context)                 { a.server.logout(c) }
func (a *ContractAdapter) ConfirmMfa(c *gin.Context)             { a.server.mfaConfirm(c) }
func (a *ContractAdapter) EnrollMfa(c *gin.Context)              { a.server.mfaEnroll(c) }
func (a *ContractAdapter) VerifyMfa(c *gin.Context)              { a.server.mfaVerify(c) }
func (a *ContractAdapter) RefreshSession(c *gin.Context)         { a.server.refresh(c) }
func (a *ContractAdapter) ListCandidates(c *gin.Context)         { a.server.listCandidates(c) }
func (a *ContractAdapter) CreateCandidate(c *gin.Context)        { a.server.createCandidate(c) }
func (a *ContractAdapter) ListConversations(c *gin.Context)      { a.server.listConversations(c) }
func (a *ContractAdapter) CreateConversation(c *gin.Context)     { a.server.createConversation(c) }
func (a *ContractAdapter) GetDashboardSummary(c *gin.Context)    { a.server.dashboardSummary(c) }
func (a *ContractAdapter) ListDepartments(c *gin.Context)        { a.server.listDepartments(c) }
func (a *ContractAdapter) CreateDepartment(c *gin.Context)       { a.server.createDepartment(c) }
func (a *ContractAdapter) CreateEmployee(c *gin.Context)         { a.server.createEmployee(c) }
func (a *ContractAdapter) ListFaqs(c *gin.Context)               { a.server.listFAQs(c) }
func (a *ContractAdapter) ListGoals(c *gin.Context)              { a.server.listGoals(c) }
func (a *ContractAdapter) CreateGoal(c *gin.Context)             { a.server.createGoal(c) }
func (a *ContractAdapter) ListIntegrations(c *gin.Context)       { a.server.listIntegrations(c) }
func (a *ContractAdapter) ListAuditLogs(c *gin.Context, _ generated.ListAuditLogsParams) {
	a.server.listAuditLogs(c)
}
func (a *ContractAdapter) ListJobPostings(c *gin.Context)     { a.server.listJobPostings(c) }
func (a *ContractAdapter) CreateJobPosting(c *gin.Context)    { a.server.createJobPosting(c) }
func (a *ContractAdapter) ListLeaveBalances(c *gin.Context)   { a.server.leaveBalances(c) }
func (a *ContractAdapter) ListLeaveRequests(c *gin.Context)   { a.server.listLeaveRequests(c) }
func (a *ContractAdapter) CreateLeaveRequest(c *gin.Context)  { a.server.createLeaveRequest(c) }
func (a *ContractAdapter) ListLeaveTypes(c *gin.Context)      { a.server.listLeaveTypes(c) }
func (a *ContractAdapter) GetSessionUser(c *gin.Context)      { a.server.me(c) }
func (a *ContractAdapter) ChangePassword(c *gin.Context)      { a.server.changePassword(c) }
func (a *ContractAdapter) GetProfile(c *gin.Context)          { a.server.profile(c) }
func (a *ContractAdapter) UpdateProfile(c *gin.Context)       { a.server.updateProfile(c) }
func (a *ContractAdapter) ListNotifications(c *gin.Context)   { a.server.listNotifications(c) }
func (a *ContractAdapter) GetOrgChart(c *gin.Context)         { a.server.orgChart(c) }
func (a *ContractAdapter) GetOrganization(c *gin.Context)     { a.server.organization(c) }
func (a *ContractAdapter) UpdateOrganization(c *gin.Context)  { a.server.updateOrganization(c) }
func (a *ContractAdapter) ListPayrollPolicies(c *gin.Context) { a.server.listPayrollPolicies(c) }
func (a *ContractAdapter) ListPayrollRuns(c *gin.Context)     { a.server.listPayrollRuns(c) }
func (a *ContractAdapter) CreatePayrollRun(c *gin.Context)    { a.server.createPayrollRun(c) }
func (a *ContractAdapter) ListPayslips(c *gin.Context)        { a.server.listPayslips(c) }
func (a *ContractAdapter) ExportPayslips(c *gin.Context, _ generated.ExportPayslipsParams) {
	a.server.exportPayslips(c)
}
func (a *ContractAdapter) ListPerformanceCycles(c *gin.Context)  { a.server.listPerformanceCycles(c) }
func (a *ContractAdapter) CreatePerformanceCycle(c *gin.Context) { a.server.createPerformanceCycle(c) }
func (a *ContractAdapter) ListPermissions(c *gin.Context)        { a.server.listPermissions(c) }
func (a *ContractAdapter) ListPositions(c *gin.Context)          { a.server.listPositions(c) }
func (a *ContractAdapter) CreatePosition(c *gin.Context)         { a.server.createPosition(c) }
func (a *ContractAdapter) CreateRealtimeTicket(c *gin.Context)   { a.server.realtimeTicket(c) }
func (a *ContractAdapter) ListReviews(c *gin.Context)            { a.server.listReviews(c) }
func (a *ContractAdapter) CreateReview(c *gin.Context)           { a.server.createReview(c) }
func (a *ContractAdapter) ListRoles(c *gin.Context)              { a.server.listRoles(c) }
func (a *ContractAdapter) ListShiftAssignments(c *gin.Context)   { a.server.listShifts(c) }
func (a *ContractAdapter) AssignShift(c *gin.Context)            { a.server.assignShift(c) }
func (a *ContractAdapter) ListSupportTickets(c *gin.Context)     { a.server.listSupportTickets(c) }
func (a *ContractAdapter) CreateSupportTicket(c *gin.Context)    { a.server.createSupportTicket(c) }
func (a *ContractAdapter) ListEmployees(c *gin.Context, _ generated.ListEmployeesParams) {
	a.server.listEmployees(c)
}
func (a *ContractAdapter) ExportEmployees(c *gin.Context, _ generated.ExportEmployeesParams) {
	a.server.exportEmployees(c)
}
func (a *ContractAdapter) ImportEmployees(c *gin.Context, _ generated.ImportEmployeesParams) {
	a.server.importEmployees(c)
}
func (a *ContractAdapter) GlobalSearch(c *gin.Context, _ generated.GlobalSearchParams) {
	a.server.globalSearch(c)
}

func (a *ContractAdapter) UpdateAttendance(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.updateAttendance(c)
}
func (a *ContractAdapter) FinishSso(c *gin.Context, provider generated.FinishSsoParamsProvider, _ generated.FinishSsoParams) {
	bindParam(c, "provider", string(provider))
	a.server.ssoCallback(c)
}
func (a *ContractAdapter) StartSso(c *gin.Context, provider generated.StartSsoParamsProvider, _ generated.StartSsoParams) {
	bindParam(c, "provider", string(provider))
	a.server.ssoStart(c)
}
func (a *ContractAdapter) MoveCandidate(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.moveCandidate(c)
}
func (a *ContractAdapter) ListMessages(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.listMessages(c)
}
func (a *ContractAdapter) CreateMessage(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.createMessage(c)
}
func (a *ContractAdapter) DismissAttention(c *gin.Context, key string) {
	bindParam(c, "key", key)
	a.server.dismissAttention(c)
}
func (a *ContractAdapter) DeleteDepartment(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.deleteDepartment(c)
}
func (a *ContractAdapter) GetEmployee(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.getEmployee(c)
}
func (a *ContractAdapter) UpdateEmployee(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.updateEmployee(c)
}
func (a *ContractAdapter) UpdateGoal(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.updateGoal(c)
}
func (a *ContractAdapter) UpdateIntegration(c *gin.Context, provider generated.UpdateIntegrationParamsProvider) {
	bindParam(c, "provider", string(provider))
	a.server.updateIntegration(c)
}
func (a *ContractAdapter) UpdateJobPosting(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.updateJobPosting(c)
}
func (a *ContractAdapter) ApproveLeave(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.approveLeave(c)
}
func (a *ContractAdapter) RejectLeave(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.rejectLeave(c)
}
func (a *ContractAdapter) DismissNotification(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.dismissNotification(c)
}
func (a *ContractAdapter) ReadNotification(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.readNotification(c)
}
func (a *ContractAdapter) GetPayrollRun(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.getPayrollRun(c)
}
func (a *ContractAdapter) ExecutePayrollRun(c *gin.Context, id generated.Id, _ generated.ExecutePayrollRunParams) {
	bindParam(c, "id", id.String())
	a.server.executePayroll(c)
}
func (a *ContractAdapter) UpdateRolePermissions(c *gin.Context, id generated.Id) {
	bindParam(c, "id", id.String())
	a.server.updateRolePermissions(c)
}
