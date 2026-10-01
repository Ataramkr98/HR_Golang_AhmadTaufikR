package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simpul/hr-backend/internal/domain"
	"github.com/simpul/hr-backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (s *Server) registerCoreRoutes(r *gin.RouterGroup) {
	r.GET("/me", s.me)
	r.GET("/me/profile", s.profile)
	r.PATCH("/me/profile", s.updateProfile)
	r.POST("/me/password", s.changePassword)
	r.GET("/organization", s.organization)
	r.PATCH("/organization", permissionRequired("settings.manage"), s.updateOrganization)
	r.GET("/departments", s.listDepartments)
	r.POST("/departments", permissionRequired("employees.manage"), s.createDepartment)
	r.DELETE("/departments/:id", permissionRequired("employees.manage"), s.deleteDepartment)
	r.GET("/positions", s.listPositions)
	r.POST("/positions", permissionRequired("employees.manage"), s.createPosition)
	r.GET("/employees", permissionRequired("employees.view"), s.listEmployees)
	// Registered before the ":id" route for readability only; Gin resolves the static
	// segment first regardless of registration order.
	r.GET("/employees/export", permissionRequired("employees.view"), s.exportEmployees)
	// The roster import writes employees, so it needs the manage permission rather than
	// the view permission its export counterpart uses.
	r.POST("/employees/import", permissionRequired("employees.manage"), s.importEmployees)
	r.POST("/employees", permissionRequired("employees.manage"), s.createEmployee)
	r.GET("/employees/:id", permissionRequired("employees.view"), s.getEmployee)
	r.PATCH("/employees/:id", permissionRequired("employees.manage"), s.updateEmployee)
	r.GET("/org-chart", permissionRequired("employees.view"), s.orgChart)
	r.GET("/roles", permissionRequired("settings.manage"), s.listRoles)
	r.GET("/permissions", permissionRequired("settings.manage"), s.listPermissions)
	r.PUT("/roles/:id/permissions", permissionRequired("settings.manage"), s.updateRolePermissions)
	r.GET("/integrations", permissionRequired("settings.manage"), s.listIntegrations)
	r.PATCH("/integrations/:provider", permissionRequired("settings.manage"), s.updateIntegration)
	r.GET("/audit-logs", permissionRequired("settings.manage"), s.listAuditLogs)
	r.GET("/dashboard/summary", s.dashboardSummary)
	r.POST("/dashboard/attention/:key/dismiss", s.dismissAttention)
	r.GET("/analytics/workforce", permissionRequired("analytics.view"), s.workforceAnalytics)
	r.GET("/search", s.globalSearch)
}

func (s *Server) me(c *gin.Context) {
	id := identityFrom(c)
	var user domain.User
	var membership domain.Membership
	if err := s.db.First(&user, "id = ?", id.UserID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := s.db.Preload("Role.Permissions").Preload("Organization").First(&membership, "id = ? AND organization_id = ?", id.MembershipID, id.OrganizationID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	permissions := make([]string, 0, len(membership.Role.Permissions))
	for _, p := range membership.Role.Permissions {
		permissions = append(permissions, p.Key)
	}
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "email": user.Email, "role": membership.Role.Key, "organization": membership.Organization, "employeeId": membership.EmployeeID, "permissions": permissions, "mfaEnabled": user.MFAEnabled})
}

func (s *Server) profile(c *gin.Context) {
	id := identityFrom(c)
	if id.EmployeeID == "" {
		problem(c, http.StatusNotFound, "profile_not_found", "Profil tidak ditemukan", "Membership tidak terhubung ke karyawan.", nil)
		return
	}
	var employee domain.Employee
	if err := s.db.Preload("Department").Preload("Position").Preload("Manager").Where("organization_id = ? AND id = ?", id.OrganizationID, id.EmployeeID).First(&employee).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, employee)
}

func (s *Server) updateProfile(c *gin.Context) {
	id := identityFrom(c)
	if id.EmployeeID == "" {
		problem(c, http.StatusNotFound, "profile_not_found", "Profil tidak ditemukan", "Membership tidak terhubung ke karyawan.", nil)
		return
	}
	var req struct {
		Phone          *string `json:"phone"`
		Address        *string `json:"address"`
		EmergencyName  *string `json:"emergencyName"`
		EmergencyPhone *string `json:"emergencyPhone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data tidak valid", err.Error(), nil)
		return
	}
	updates := map[string]any{}
	if req.Phone != nil {
		updates["phone"] = strings.TrimSpace(*req.Phone)
	}
	if req.Address != nil {
		updates["address"] = strings.TrimSpace(*req.Address)
	}
	if req.EmergencyName != nil {
		updates["emergency_name"] = strings.TrimSpace(*req.EmergencyName)
	}
	if req.EmergencyPhone != nil {
		updates["emergency_phone"] = strings.TrimSpace(*req.EmergencyPhone)
	}
	if len(updates) == 0 {
		problem(c, http.StatusBadRequest, "no_changes", "Tidak ada perubahan", "Kirim sedikitnya satu field.", nil)
		return
	}
	var employee domain.Employee
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Where("organization_id = ? AND id = ?", id.OrganizationID, id.EmployeeID).First(&employee).Error; err != nil {
			return err
		}
		before := employee
		if err := tx.Model(&employee).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&employee, "id = ?", employee.ID).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "update", "employee", employee.ID, before, employee)
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, employee)
}

func (s *Server) changePassword(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword" binding:"required,min=12,max=128"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Kata sandi tidak valid", err.Error(), nil)
		return
	}
	var user domain.User
	if err := s.db.First(&user, "id = ?", id.UserID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		problem(c, http.StatusUnauthorized, "invalid_current_password", "Kata sandi saat ini salah", "Periksa kembali kata sandi saat ini.", nil)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		problem(c, http.StatusInternalServerError, "password_hash_failed", "Perubahan gagal", err.Error(), nil)
		return
	}
	err = s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Model(&user).Update("password_hash", string(hash)).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&domain.RefreshSession{}).Where("user_id = ?", user.ID).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "password_changed", "user", user.ID, nil, map[string]any{"sessionsRevoked": true})
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	s.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

func (s *Server) organization(c *gin.Context) {
	id := identityFrom(c)
	var item domain.Organization
	if err := s.db.First(&item, "id = ?", id.OrganizationID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) updateOrganization(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		Name     *string `json:"name"`
		Industry *string `json:"industry"`
		Address  *string `json:"address"`
		Phone    *string `json:"phone"`
		Email    *string `json:"email"`
		Timezone *string `json:"timezone"`
		Locale   *string `json:"locale"`
		Currency *string `json:"currency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data tidak valid", err.Error(), nil)
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Industry != nil {
		updates["industry"] = *req.Industry
	}
	if req.Address != nil {
		updates["address"] = *req.Address
	}
	if req.Phone != nil {
		updates["phone"] = *req.Phone
	}
	if req.Email != nil {
		updates["email"] = *req.Email
	}
	if req.Timezone != nil {
		updates["timezone"] = *req.Timezone
	}
	if req.Locale != nil {
		updates["locale"] = *req.Locale
	}
	if req.Currency != nil {
		updates["currency"] = *req.Currency
	}
	var item domain.Organization
	if err := s.db.First(&item, "id = ?", id.OrganizationID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	before := item
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&item, "id = ?", item.ID).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "update", "organization", item.ID, before, item)
	}); err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) listDepartments(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Department
	if err := s.db.Where("organization_id = ?", id.OrganizationID).Order("name").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	for i := range items {
		s.db.Model(&domain.Employee{}).Where("organization_id = ? AND department_id = ? AND deleted_at IS NULL", id.OrganizationID, items[i].ID).Count(&items[i].EmployeeCount)
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: 1, PageSize: len(items), Total: int64(len(items))}})
}

func (s *Server) createDepartment(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		Name               string  `json:"name" binding:"required,min=1,max=120"`
		ParentDepartmentID *string `json:"parentDepartmentId"`
		HeadEmployeeID     *string `json:"headEmployeeId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Departemen tidak valid", err.Error(), nil)
		return
	}
	if req.ParentDepartmentID != nil && !tenantReferenceExists(s.db, &domain.Department{}, id.OrganizationID, *req.ParentDepartmentID) {
		problem(c, http.StatusBadRequest, "invalid_parent_department", "Departemen induk tidak valid", "Departemen induk harus berasal dari workspace yang sama.", nil)
		return
	}
	if req.HeadEmployeeID != nil && !tenantReferenceExists(s.db, &domain.Employee{}, id.OrganizationID, *req.HeadEmployeeID) {
		problem(c, http.StatusBadRequest, "invalid_department_head", "Kepala departemen tidak valid", "Karyawan harus berasal dari workspace yang sama.", nil)
		return
	}
	item := domain.Department{OrganizationID: id.OrganizationID, Name: strings.TrimSpace(req.Name), ParentDepartmentID: req.ParentDepartmentID, HeadEmployeeID: req.HeadEmployeeID}
	if err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "create", "department", item.ID, nil, item)
	}); err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (s *Server) deleteDepartment(c *gin.Context) {
	id := identityFrom(c)
	if !validUUID(c.Param("id")) {
		problem(c, http.StatusBadRequest, "invalid_id", "ID tidak valid", "Gunakan UUID yang valid.", nil)
		return
	}
	var count int64
	s.db.Model(&domain.Employee{}).Where("organization_id = ? AND department_id = ?", id.OrganizationID, c.Param("id")).Count(&count)
	if count > 0 {
		problem(c, http.StatusConflict, "department_in_use", "Departemen masih digunakan", "Pindahkan karyawan sebelum menghapus departemen.", nil)
		return
	}
	var posCount int64
	s.db.Model(&domain.Position{}).Where("organization_id = ? AND department_id = ?", id.OrganizationID, c.Param("id")).Count(&posCount)
	if posCount > 0 {
		problem(c, http.StatusConflict, "department_has_positions", "Departemen masih memiliki jabatan", "Hapus atau pindahkan jabatan sebelum menghapus departemen.", nil)
		return
	}
	var jobCount int64
	s.db.Model(&domain.JobPosting{}).Where("organization_id = ? AND department_id = ?", id.OrganizationID, c.Param("id")).Count(&jobCount)
	if jobCount > 0 {
		problem(c, http.StatusConflict, "department_has_job_postings", "Departemen masih memiliki lowongan kerja", "Hapus atau pindahkan lowongan sebelum menghapus departemen.", nil)
		return
	}
	result := s.db.Where("organization_id = ? AND id = ?", id.OrganizationID, c.Param("id")).Delete(&domain.Department{})
	if result.Error != nil {
		databaseProblem(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		databaseProblem(c, gorm.ErrRecordNotFound)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listPositions(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Position
	q := s.db.Preload("Department").Where("positions.organization_id = ?", id.OrganizationID)
	if dept := c.Query("departmentId"); dept != "" {
		q = q.Where("positions.department_id = ?", dept)
	}
	if err := q.Order("title").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: 1, PageSize: len(items), Total: int64(len(items))}})
}

func (s *Server) createPosition(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		DepartmentID string `json:"departmentId" binding:"required,uuid"`
		Title        string `json:"title" binding:"required,max=120"`
		Level        string `json:"level"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Jabatan tidak valid", err.Error(), nil)
		return
	}
	if !tenantReferenceExists(s.db, &domain.Department{}, id.OrganizationID, req.DepartmentID) {
		problem(c, http.StatusBadRequest, "invalid_department", "Departemen tidak valid", "Departemen harus berasal dari workspace yang sama.", nil)
		return
	}
	item := domain.Position{OrganizationID: id.OrganizationID, DepartmentID: req.DepartmentID, Title: strings.TrimSpace(req.Title), Level: req.Level}
	if err := s.db.Create(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// employeeQuery applies the tenant scoping and filters shared by the paginated list and
// the CSV export, so an export can never drift from what the table displays.
func (s *Server) employeeQuery(c *gin.Context, id Identity) *gorm.DB {
	q := s.db.Model(&domain.Employee{}).Where("employees.organization_id = ? AND employees.deleted_at IS NULL", id.OrganizationID)
	if id.Role == "team_lead" && id.EmployeeID != "" {
		q = q.Where("employees.manager_id = ? OR employees.id = ?", id.EmployeeID, id.EmployeeID)
	} else if id.Role == "employee" && id.EmployeeID != "" {
		q = q.Where("employees.id = ?", id.EmployeeID)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		// Position is matched through EXISTS rather than a JOIN: a join would emit one row
		// per matching position and silently inflate the `Count` used for pagination.
		// Keeping the same four fields as the directory's client-side filter means the CSV
		// export cannot return a different set of people than the table shows.
		q = q.Where(
			"lower(full_name) LIKE ? OR lower(employee_code) LIKE ? OR lower(email) LIKE ?"+
				" OR EXISTS (SELECT 1 FROM positions p WHERE p.id = employees.position_id AND lower(p.title) LIKE ?)",
			like, like, like, like,
		)
	}
	if dept := c.Query("departmentId"); dept != "" {
		q = q.Where("department_id = ?", dept)
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	return q
}

func (s *Server) listEmployees(c *gin.Context) {
	id := identityFrom(c)
	page, size := pagination(c)
	var items []domain.Employee
	var total int64
	q := s.employeeQuery(c, id)
	if err := q.Count(&total).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := q.Preload("Department").Preload("Position").Preload("Manager").Preload("Compensation").Order("full_name").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}})
}

// exportEmployees streams the full filtered roster. It deliberately ignores pagination:
// an export is expected to contain every row matching the current filters, not one page.
func (s *Server) exportEmployees(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Employee
	if err := s.employeeQuery(c, id).
		Preload("Department").Preload("Position").Preload("Manager").
		Order("full_name").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		manager := ""
		if item.Manager != nil {
			manager = item.Manager.FullName
		}
		rows = append(rows, []string{
			item.FullName,
			item.EmployeeCode,
			item.Email,
			item.Phone,
			item.Department.Name,
			item.Position.Title,
			manager,
			item.Status,
			formatCSVDate(item.HireDate),
		})
	}
	writeCSV(c, "karyawan.csv",
		[]string{"Nama", "Kode Karyawan", "Email", "Telepon", "Departemen", "Jabatan", "Manajer", "Status", "Tanggal Masuk"},
		rows)
}

type employeeRequest struct {
	EmployeeCode          string  `json:"employeeCode"`
	FullName              string  `json:"fullName" binding:"required,min=1,max=120"`
	Email                 string  `json:"email" binding:"required,email"`
	Phone                 string  `json:"phone"`
	HireDate              string  `json:"hireDate" binding:"required"`
	Status                string  `json:"status" binding:"required,oneof=active on_leave inactive terminated"`
	DepartmentID          string  `json:"departmentId" binding:"required,uuid"`
	PositionID            string  `json:"positionId" binding:"required,uuid"`
	ManagerID             *string `json:"managerId"`
	Address               string  `json:"address"`
	Education             string  `json:"education"`
	Gender                string  `json:"gender"`
	NIK                   string  `json:"nik"`
	BaseSalary            int64   `json:"baseSalary" binding:"gte=0"`
	TaxStatus             string  `json:"taxStatus"`
	BPJSHealthEnabled     *bool   `json:"bpjsHealthEnabled"`
	BPJSEmploymentEnabled *bool   `json:"bpjsEmploymentEnabled"`
	BankName              string  `json:"bankName"`
	BankAccount           string  `json:"bankAccount"`
}

func (s *Server) createEmployee(c *gin.Context) {
	id := identityFrom(c)
	var req employeeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data karyawan tidak valid", err.Error(), nil)
		return
	}
	hireDate, err := parseDate(req.HireDate)
	if err != nil {
		problem(c, http.StatusBadRequest, "invalid_hire_date", "Tanggal bergabung tidak valid", "Gunakan YYYY-MM-DD.", nil)
		return
	}
	var relationCount int64
	s.db.Model(&domain.Position{}).Where("organization_id=? AND id=? AND department_id=?", id.OrganizationID, req.PositionID, req.DepartmentID).Count(&relationCount)
	if relationCount != 1 {
		problem(c, http.StatusBadRequest, "invalid_position", "Jabatan tidak valid", "Jabatan dan departemen harus berasal dari workspace yang sama.", nil)
		return
	}
	if req.ManagerID != nil && !tenantReferenceExists(s.db, &domain.Employee{}, id.OrganizationID, *req.ManagerID) {
		problem(c, http.StatusBadRequest, "invalid_manager", "Manager tidak valid", "Manager harus berasal dari workspace yang sama.", nil)
		return
	}
	nikEnc, err := s.cipher.Encrypt(req.NIK)
	if err != nil {
		problem(c, http.StatusInternalServerError, "encryption_failed", "Data gagal dienkripsi", err.Error(), nil)
		return
	}
	bankEnc, err := s.cipher.Encrypt(req.BankAccount)
	if err != nil {
		problem(c, http.StatusInternalServerError, "encryption_failed", "Data gagal dienkripsi", err.Error(), nil)
		return
	}
	code := strings.TrimSpace(req.EmployeeCode)
	if code == "" {
		var next int
		s.db.Raw("SELECT COALESCE(MAX(CASE WHEN employee_code ~ '^EMP-[0-9]+$' THEN substring(employee_code from 5)::int ELSE 0 END),0)+1 FROM employees WHERE organization_id = ?", id.OrganizationID).Scan(&next)
		code = fmt.Sprintf("EMP-%04d", next)
	}
	employee := domain.Employee{OrganizationID: id.OrganizationID, EmployeeCode: code, FullName: strings.TrimSpace(req.FullName), Email: strings.ToLower(strings.TrimSpace(req.Email)), Phone: req.Phone, HireDate: hireDate, Status: req.Status, DepartmentID: req.DepartmentID, PositionID: req.PositionID, ManagerID: req.ManagerID, Address: req.Address, Education: req.Education, Gender: req.Gender, NIKEncrypted: nikEnc}
	health, employment := true, true
	if req.BPJSHealthEnabled != nil {
		health = *req.BPJSHealthEnabled
	}
	if req.BPJSEmploymentEnabled != nil {
		employment = *req.BPJSEmploymentEnabled
	}
	taxStatus := req.TaxStatus
	if taxStatus == "" {
		taxStatus = "TK/0"
	}
	err = s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Create(&employee).Error; err != nil {
			return err
		}
		comp := domain.Compensation{OrganizationID: id.OrganizationID, EmployeeID: employee.ID, BaseSalary: req.BaseSalary, TaxStatus: taxStatus, BPJSHealthEnabled: health, BPJSEmploymentEnabled: employment, BankName: req.BankName, BankAccountEnc: bankEnc, EffectiveFrom: hireDate}
		if err := tx.Create(&comp).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "create", "employee", employee.ID, nil, employee)
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	_ = s.db.Preload("Department").Preload("Position").Preload("Manager").Preload("Compensation").First(&employee, "id = ?", employee.ID).Error
	c.JSON(http.StatusCreated, employee)
}

func (s *Server) getEmployee(c *gin.Context) {
	id := identityFrom(c)
	if !s.employeeScopeAllowed(id, c.Param("id")) {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Karyawan berada di luar cakupan Anda.", nil)
		return
	}
	var item domain.Employee
	if err := s.db.Preload("Department").Preload("Position").Preload("Manager").Preload("Compensation").Where("organization_id = ? AND id = ? AND deleted_at IS NULL", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) updateEmployee(c *gin.Context) {
	id := identityFrom(c)
	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Data tidak valid", err.Error(), nil)
		return
	}
	allowed := map[string]string{"fullName": "full_name", "email": "email", "phone": "phone", "status": "status", "departmentId": "department_id", "positionId": "position_id", "managerId": "manager_id", "address": "address", "education": "education", "gender": "gender"}
	updates := map[string]any{}
	for key, column := range allowed {
		if value, ok := req[key]; ok {
			updates[column] = value
		}
	}
	var item domain.Employee
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Where("organization_id = ? AND id = ?", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
			return err
		}
		departmentID, positionID := item.DepartmentID, item.PositionID
		if value, ok := updates["department_id"].(string); ok {
			departmentID = value
		}
		if value, ok := updates["position_id"].(string); ok {
			positionID = value
		}
		var positionCount int64
		tx.Model(&domain.Position{}).Where("organization_id=? AND id=? AND department_id=?", id.OrganizationID, positionID, departmentID).Count(&positionCount)
		if positionCount != 1 {
			return fmt.Errorf("employee position is outside tenant or department")
		}
		if manager, exists := updates["manager_id"]; exists && manager != nil && manager != "" {
			managerID, ok := manager.(string)
			if !ok || !tenantReferenceExists(tx, &domain.Employee{}, id.OrganizationID, managerID) || managerID == item.ID {
				return fmt.Errorf("employee manager is invalid")
			}
		}
		before := item
		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&item, "id = ?", item.ID).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "update", "employee", item.ID, before, item)
	})
	if err != nil {
		if strings.Contains(err.Error(), "employee position") || strings.Contains(err.Error(), "employee manager") {
			problem(c, http.StatusBadRequest, "invalid_employee_relation", "Relasi karyawan tidak valid", err.Error(), nil)
		} else {
			databaseProblem(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) employeeScopeAllowed(id Identity, target string) bool {
	if id.Role == "hr_admin" || id.Role == "finance_manager" {
		return true
	}
	if target == id.EmployeeID {
		return true
	}
	if id.Role == "team_lead" {
		var count int64
		s.db.Model(&domain.Employee{}).Where("organization_id = ? AND id = ? AND manager_id = ?", id.OrganizationID, target, id.EmployeeID).Count(&count)
		return count > 0
	}
	return false
}

func (s *Server) orgChart(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Employee
	if err := s.db.Preload("Department").Preload("Position").Where("organization_id = ? AND deleted_at IS NULL AND status <> 'terminated'", id.OrganizationID).Order("full_name").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	nodes := make([]gin.H, 0, len(items))
	for _, e := range items {
		nodes = append(nodes, gin.H{"id": e.ID, "employeeCode": e.EmployeeCode, "name": e.FullName, "role": e.Position.Title, "department": e.Department.Name, "managerId": e.ManagerID})
	}
	c.JSON(http.StatusOK, gin.H{"data": nodes})
}

func (s *Server) listRoles(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Role
	if err := s.db.Preload("Permissions").Where("organization_id = ?", id.OrganizationID).Order("name").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) listPermissions(c *gin.Context) {
	var items []domain.Permission
	if err := s.db.Order("key").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) updateRolePermissions(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		PermissionKeys []string `json:"permissionKeys" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Permission tidak valid", err.Error(), nil)
		return
	}
	var role domain.Role
	if err := s.db.Preload("Permissions").Where("organization_id = ? AND id = ?", id.OrganizationID, c.Param("id")).First(&role).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	var permissions []domain.Permission
	if err := s.db.Where("key IN ?", req.PermissionKeys).Find(&permissions).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if len(permissions) != len(req.PermissionKeys) {
		problem(c, http.StatusBadRequest, "unknown_permission", "Permission tidak dikenal", "Satu atau lebih permission key tidak tersedia.", nil)
		return
	}
	before := permissionKeys(role.Permissions)
	if err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Model(&role).Association("Permissions").Replace(permissions); err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "permissions_changed", "role", role.ID, before, req.PermissionKeys)
	}); err != nil {
		databaseProblem(c, err)
		return
	}
	role.Permissions = permissions
	c.JSON(http.StatusOK, role)
}

func (s *Server) listIntegrations(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Integration
	if err := s.db.Where("organization_id = ?", id.OrganizationID).Order("provider").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, gin.H{"id": item.ID, "provider": item.Provider, "enabled": item.Enabled, "configured": item.ConfigEnc != "" || providerConfigured(s.config, item.Provider)})
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// listAuditLogs exposes the append-only audit trail that every mutating handler
// writes through repository.Audit. It is restricted to administrators because the
// entries describe who changed what, and it deliberately omits the before/after
// payloads so the endpoint never echoes sensitive field values back to a client.
func (s *Server) listAuditLogs(c *gin.Context) {
	id := identityFrom(c)
	page, pageSize := pagination(c)

	q := s.db.Model(&domain.AuditLog{}).Where("organization_id = ?", id.OrganizationID)
	if entityType := strings.TrimSpace(c.Query("entityType")); entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	if action := strings.TrimSpace(c.Query("action")); action != "" {
		q = q.Where("action = ?", action)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	var items []domain.AuditLog
	if err := q.Order("created_at DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}

	// Resolve actor identities in a single extra round trip so the UI can show who
	// performed each action instead of a bare UUID.
	actorIDs := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item.ActorUserID != nil && !seen[*item.ActorUserID] {
			seen[*item.ActorUserID] = true
			actorIDs = append(actorIDs, *item.ActorUserID)
		}
	}
	emails := make(map[string]string, len(actorIDs))
	if len(actorIDs) > 0 {
		var users []domain.User
		if err := s.db.Where("id IN ?", actorIDs).Find(&users).Error; err == nil {
			for _, user := range users {
				emails[user.ID] = user.Email
			}
		}
	}

	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		actorEmail := ""
		if item.ActorUserID != nil {
			actorEmail = emails[*item.ActorUserID]
		}
		result = append(result, gin.H{
			"id":          item.ID,
			"action":      item.Action,
			"entityType":  item.EntityType,
			"entityId":    item.EntityID,
			"actorUserId": item.ActorUserID,
			"actorEmail":  actorEmail,
			"requestId":   item.RequestID,
			"createdAt":   item.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": result, "meta": listMeta{Page: page, PageSize: pageSize, Total: total}})
}
func (s *Server) updateIntegration(c *gin.Context) {
	id := identityFrom(c)
	provider := strings.ToLower(c.Param("provider"))
	if provider != "slack" && provider != "google" && provider != "entra" {
		problem(c, http.StatusBadRequest, "unsupported_provider", "Provider tidak didukung", "Gunakan slack, google, atau entra.", nil)
		return
	}
	var req struct {
		Enabled bool           `json:"enabled"`
		Config  map[string]any `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Konfigurasi tidak valid", err.Error(), nil)
		return
	}
	item := domain.Integration{OrganizationID: id.OrganizationID, Provider: provider}
	err := s.db.Where("organization_id = ? AND provider = ?", id.OrganizationID, provider).First(&item).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		databaseProblem(c, err)
		return
	}
	existingConfig := item.ConfigEnc
	encrypted := existingConfig
	if len(req.Config) > 0 {
		if provider == "slack" {
			webhook, _ := req.Config["webhookUrl"].(string)
			if !strings.HasPrefix(webhook, "https://hooks.slack.com/") {
				problem(c, http.StatusBadRequest, "invalid_webhook", "Webhook Slack tidak valid", "Gunakan URL HTTPS dari hooks.slack.com.", nil)
				return
			}
		}
		raw, marshalErr := json.Marshal(req.Config)
		if marshalErr != nil {
			problem(c, http.StatusBadRequest, "invalid_config", "Konfigurasi tidak valid", marshalErr.Error(), nil)
			return
		}
		encrypted, err = s.cipher.Encrypt(string(raw))
		if err != nil {
			problem(c, http.StatusInternalServerError, "encryption_failed", "Konfigurasi gagal disimpan", err.Error(), nil)
			return
		}
	}
	configured := encrypted != "" || providerConfigured(s.config, provider)
	if req.Enabled && !configured {
		problem(c, http.StatusConflict, "provider_not_configured", "Integrasi belum dikonfigurasi", "Tambahkan credential provider sebelum mengaktifkannya.", nil)
		return
	}
	before := gin.H{"enabled": item.Enabled, "configured": existingConfig != "" || providerConfigured(s.config, provider)}
	err = s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		item.OrganizationID = id.OrganizationID
		item.Provider = provider
		item.Enabled = req.Enabled
		item.ConfigEnc = encrypted
		if err := tx.Where("organization_id = ? AND provider = ?", id.OrganizationID, provider).Save(&item).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "integration_changed", "integration", item.ID, before, gin.H{"enabled": item.Enabled, "configured": configured})
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": item.ID, "provider": provider, "enabled": req.Enabled, "configured": configured})
}

func permissionKeys(permissions []domain.Permission) []string {
	keys := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		keys = append(keys, permission.Key)
	}
	return keys
}

func (s *Server) dashboardSummary(c *gin.Context) {
	id := identityFrom(c)
	cacheKey := "simpul:cache:" + id.OrganizationID + ":dashboard:" + id.UserID
	if s.serveCachedJSON(c, cacheKey) {
		return
	}
	var active, pending int64
	s.db.Model(&domain.Employee{}).Where("organization_id = ? AND status = 'active' AND deleted_at IS NULL", id.OrganizationID).Count(&active)
	s.db.Model(&domain.LeaveRequest{}).Where("organization_id = ? AND status = 'pending'", id.OrganizationID).Count(&pending)
	var run domain.PayrollRun
	_ = s.db.Where("organization_id = ?", id.OrganizationID).Order("period_end DESC").First(&run).Error
	type point struct {
		Date       time.Time
		Attendance int
		Overtime   int
	}
	var points []point
	s.db.Raw(`SELECT date, COUNT(*) FILTER (WHERE status IN ('present','late')) AS attendance, COALESCE(SUM(overtime_minutes),0) AS overtime FROM attendances WHERE organization_id = ? AND date >= CURRENT_DATE - INTERVAL '6 days' GROUP BY date ORDER BY date`, id.OrganizationID).Scan(&points)
	// Always emit a dense 7-day series so the chart axis and the plotted line agree.
	byDate := make(map[string]point, len(points))
	for _, p := range points {
		byDate[p.Date.Format("2006-01-02")] = p
	}
	today := time.Now().In(s.organizationLocation(id.OrganizationID))
	activity := make([]gin.H, 0, 7)
	for offset := 6; offset >= 0; offset-- {
		day := today.AddDate(0, 0, -offset).Format("2006-01-02")
		p := byDate[day]
		activity = append(activity, gin.H{"date": day, "attendance": p.Attendance, "overtimeMinutes": p.Overtime})
	}
	attention := s.attentionItems(id)
	// Real supporting metrics for the KPI cards — no synthesised numbers.
	var newHires, departmentCount, onLeave int64
	s.db.Model(&domain.Employee{}).Where("organization_id = ? AND deleted_at IS NULL AND hire_date >= date_trunc('month', CURRENT_DATE)", id.OrganizationID).Count(&newHires)
	s.db.Model(&domain.Department{}).Where("organization_id = ?", id.OrganizationID).Count(&departmentCount)
	s.db.Model(&domain.Employee{}).Where("organization_id = ? AND status = 'on_leave' AND deleted_at IS NULL", id.OrganizationID).Count(&onLeave)
	response := gin.H{
		"payroll": run, "pendingLeave": pending, "activeEmployees": active, "weeklyActivity": activity,
		"attention": attention, "newHiresThisMonth": newHires, "departmentCount": departmentCount, "onLeave": onLeave,
	}
	s.writeCachedJSON(c, cacheKey, response, 20*time.Second)
	c.JSON(http.StatusOK, response)
}

func (s *Server) attentionItems(id Identity) []gin.H {
	var dismissed []string
	s.db.Model(&domain.AttentionDismissal{}).Where("organization_id = ? AND user_id = ?", id.OrganizationID, id.UserID).Pluck("attention_key", &dismissed)
	skip := map[string]bool{}
	for _, key := range dismissed {
		skip[key] = true
	}
	items := []gin.H{}
	var expiring int64
	s.db.Model(&domain.Employee{}).Where("organization_id = ? AND end_date BETWEEN CURRENT_DATE AND CURRENT_DATE + INTERVAL '30 days'", id.OrganizationID).Count(&expiring)
	if expiring > 0 && !skip["contracts_expiring"] {
		items = append(items, gin.H{"key": "contracts_expiring", "title": "Kontrak Berakhir", "description": fmt.Sprintf("%d karyawan memerlukan perpanjangan.", expiring), "tone": "urgent"})
	}
	var pending int64
	s.db.Model(&domain.LeaveRequest{}).Where("organization_id = ? AND status = 'pending'", id.OrganizationID).Count(&pending)
	if pending > 0 && !skip["leave_pending"] {
		items = append(items, gin.H{"key": "leave_pending", "title": "Persetujuan Cuti", "description": fmt.Sprintf("%d pengajuan menunggu keputusan.", pending), "tone": "attention"})
	}
	return items
}
func (s *Server) dismissAttention(c *gin.Context) {
	id := identityFrom(c)
	key := strings.TrimSpace(c.Param("key"))
	item := domain.AttentionDismissal{OrganizationID: id.OrganizationID, UserID: id.UserID, AttentionKey: key, DismissedAt: time.Now().UTC()}
	if err := s.db.Where("user_id = ? AND attention_key = ?", id.UserID, key).FirstOrCreate(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if s.redis != nil {
		_ = s.redis.Del(c.Request.Context(), "simpul:cache:"+id.OrganizationID+":dashboard:"+id.UserID).Err()
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) workforceAnalytics(c *gin.Context) {
	id := identityFrom(c)
	cacheKey := "simpul:cache:" + id.OrganizationID + ":workforce"
	if s.serveCachedJSON(c, cacheKey) {
		return
	}
	var headcount int64
	s.db.Model(&domain.Employee{}).Where("organization_id = ? AND status = 'active' AND deleted_at IS NULL", id.OrganizationID).Count(&headcount)
	type deptRow struct {
		Name  string
		Count int64
	}
	var departments []deptRow
	s.db.Raw(`SELECT d.name, COUNT(e.id) count FROM departments d LEFT JOIN employees e ON e.department_id=d.id AND e.deleted_at IS NULL AND e.status='active' WHERE d.organization_id=? GROUP BY d.id,d.name ORDER BY count DESC`, id.OrganizationID).Scan(&departments)
	var attendanceRate float64
	s.db.Raw(`SELECT COALESCE(100.0 * COUNT(*) FILTER (WHERE status IN ('present','late')) / NULLIF(COUNT(*),0),0) FROM attendances WHERE organization_id=? AND date>=date_trunc('month',CURRENT_DATE)`, id.OrganizationID).Scan(&attendanceRate)
	var retained, base int64
	s.db.Model(&domain.Employee{}).Where("organization_id=? AND hire_date < date_trunc('year',CURRENT_DATE)", id.OrganizationID).Count(&base)
	s.db.Model(&domain.Employee{}).Where("organization_id=? AND hire_date < date_trunc('year',CURRENT_DATE) AND (end_date IS NULL OR end_date >= CURRENT_DATE)", id.OrganizationID).Count(&retained)
	retention := 0.0
	if base > 0 {
		retention = 100 * float64(retained) / float64(base)
	}
	var male, female int64
	s.db.Model(&domain.Employee{}).Where("organization_id=? AND status='active' AND deleted_at IS NULL AND lower(gender)='male'", id.OrganizationID).Count(&male)
	s.db.Model(&domain.Employee{}).Where("organization_id=? AND status='active' AND deleted_at IS NULL AND lower(gender)='female'", id.OrganizationID).Count(&female)
	trend := s.headcountTrend(id.OrganizationID)
	response := gin.H{
		"headcount": headcount, "retentionRate": retention, "attendanceRate": attendanceRate,
		"departments": departments, "demographics": gin.H{"male": male, "female": female},
		"headcountTrend": trend,
	}
	s.writeCachedJSON(c, cacheKey, response, 30*time.Second)
	c.JSON(http.StatusOK, response)
}

type headcountPoint struct {
	Month     string `json:"month"`
	Headcount int64  `json:"headcount"`
}

// headcountTrend reports real month-end headcount for the trailing six months,
// derived from hire and end dates rather than a synthesised growth curve.
func (s *Server) headcountTrend(organizationID string) []headcountPoint {
	location := s.organizationLocation(organizationID)
	now := time.Now().In(location)
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	trend := make([]headcountPoint, 0, 6)
	for offset := 5; offset >= 0; offset-- {
		monthStart := currentMonth.AddDate(0, -offset, 0)
		monthEnd := monthStart.AddDate(0, 1, 0)
		var count int64
		s.db.Model(&domain.Employee{}).
			Where("organization_id = ? AND deleted_at IS NULL AND hire_date < ?", organizationID, monthEnd).
			Where("(end_date IS NULL OR end_date >= ?)", monthStart).
			Count(&count)
		trend = append(trend, headcountPoint{Month: monthStart.Format("2006-01"), Headcount: count})
	}
	return trend
}

func (s *Server) globalSearch(c *gin.Context) {
	id := identityFrom(c)
	query := strings.TrimSpace(c.Query("q"))
	if len(query) < 2 {
		c.JSON(http.StatusOK, gin.H{"data": []any{}})
		return
	}
	like := "%" + strings.ToLower(query) + "%"
	var employees []domain.Employee
	s.db.Select("id,employee_code,full_name,email").Where("organization_id=? AND deleted_at IS NULL AND (lower(full_name) LIKE ? OR lower(email) LIKE ?)", id.OrganizationID, like, like).Limit(8).Find(&employees)
	var candidates []domain.Candidate
	s.db.Select("id,full_name,email,stage").Where("organization_id=? AND (lower(full_name) LIKE ? OR lower(email) LIKE ?)", id.OrganizationID, like, like).Limit(8).Find(&candidates)
	results := []gin.H{}
	for _, e := range employees {
		results = append(results, gin.H{"type": "employee", "id": e.ID, "title": e.FullName, "subtitle": e.EmployeeCode})
	}
	for _, candidate := range candidates {
		results = append(results, gin.H{"type": "candidate", "id": candidate.ID, "title": candidate.FullName, "subtitle": candidate.Stage})
	}
	c.JSON(http.StatusOK, gin.H{"data": results})
}

func intQuery(c *gin.Context, key string, defaultValue int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return defaultValue
	}
	return value
}
