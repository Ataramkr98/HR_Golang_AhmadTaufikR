package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simpul/hr-backend/internal/domain"
	performancecalc "github.com/simpul/hr-backend/internal/modules/performance"
	recruitmentcalc "github.com/simpul/hr-backend/internal/modules/recruitment"
	"github.com/simpul/hr-backend/internal/repository"
	"gorm.io/gorm"
)

func (s *Server) registerTalentRoutes(r *gin.RouterGroup) {
	r.GET("/job-postings", permissionRequired("recruitment.view"), s.listJobPostings)
	r.POST("/job-postings", permissionRequired("recruitment.manage"), s.createJobPosting)
	r.PATCH("/job-postings/:id", permissionRequired("recruitment.manage"), s.updateJobPosting)
	r.GET("/candidates", permissionRequired("recruitment.view"), s.listCandidates)
	r.POST("/candidates", permissionRequired("recruitment.manage"), s.createCandidate)
	r.PATCH("/candidates/:id/stage", permissionRequired("recruitment.manage"), s.moveCandidate)
	r.GET("/performance-cycles", permissionRequired("performance.view"), s.listPerformanceCycles)
	r.POST("/performance-cycles", permissionRequired("performance.manage"), s.createPerformanceCycle)
	r.GET("/goals", permissionRequired("performance.view"), s.listGoals)
	r.POST("/goals", permissionRequired("performance.manage"), s.createGoal)
	r.PATCH("/goals/:id", permissionRequired("performance.manage"), s.updateGoal)
	r.GET("/reviews", permissionRequired("performance.view"), s.listReviews)
	r.POST("/reviews", permissionRequired("performance.manage"), s.createReview)
}

func (s *Server) listJobPostings(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.JobPosting
	q := s.db.Preload("Department").Where("job_postings.organization_id=?", id.OrganizationID)
	if status := c.Query("status"); status != "" {
		q = q.Where("job_postings.status=?", status)
	}
	if err := q.Order("created_at DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) createJobPosting(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		DepartmentID string `json:"departmentId" binding:"required,uuid"`
		Title        string `json:"title" binding:"required,min=2,max=120"`
		Status       string `json:"status" binding:"required,oneof=draft open closed"`
		Description  string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Lowongan tidak valid", err.Error(), nil)
		return
	}
	if !tenantReferenceExists(s.db, &domain.Department{}, id.OrganizationID, req.DepartmentID) {
		problem(c, http.StatusBadRequest, "invalid_department", "Departemen tidak valid", "Departemen harus berasal dari workspace yang sama.", nil)
		return
	}
	item := domain.JobPosting{OrganizationID: id.OrganizationID, DepartmentID: req.DepartmentID, Title: strings.TrimSpace(req.Title), Status: req.Status, Description: req.Description}
	if err := s.db.Create(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}
func (s *Server) updateJobPosting(c *gin.Context) {
	id := identityFrom(c)
	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Lowongan tidak valid", err.Error(), nil)
		return
	}
	updates := map[string]any{}
	for key, column := range map[string]string{"departmentId": "department_id", "title": "title", "status": "status", "description": "description"} {
		if value, ok := req[key]; ok {
			updates[column] = value
		}
	}
	var item domain.JobPosting
	if err := s.db.Where("organization_id=? AND id=?", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if departmentID, ok := updates["department_id"].(string); ok && !tenantReferenceExists(s.db, &domain.Department{}, id.OrganizationID, departmentID) {
		problem(c, http.StatusBadRequest, "invalid_department", "Departemen tidak valid", "Departemen harus berasal dari workspace yang sama.", nil)
		return
	}
	if err := s.db.Model(&item).Updates(updates).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := s.db.First(&item, "id=?", item.ID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) listCandidates(c *gin.Context) {
	id := identityFrom(c)
	page, size := pagination(c)
	var items []domain.Candidate
	var total int64
	q := s.db.Model(&domain.Candidate{}).Where("candidates.organization_id=?", id.OrganizationID)
	if stage := c.Query("stage"); stage != "" {
		q = q.Where("candidates.stage=?", stage)
	}
	if job := c.Query("jobPostingId"); job != "" {
		q = q.Where("candidates.job_posting_id=?", job)
	}
	q.Count(&total)
	if err := q.Preload("JobPosting").Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}, "stages": []string{"inbox", "screening", "interview", "offer", "hired"}})
}
func (s *Server) createCandidate(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		JobPostingID string `json:"jobPostingId" binding:"required,uuid"`
		FullName     string `json:"fullName" binding:"required,min=2,max=120"`
		Email        string `json:"email" binding:"required,email"`
		Source       string `json:"source"`
		Rating       int    `json:"rating" binding:"gte=0,lte=5"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Kandidat tidak valid", err.Error(), nil)
		return
	}
	var activeJob int64
	s.db.Model(&domain.JobPosting{}).Where("organization_id=? AND id=? AND status='open'", id.OrganizationID, req.JobPostingID).Count(&activeJob)
	if activeJob != 1 {
		problem(c, http.StatusBadRequest, "invalid_job_posting", "Lowongan tidak valid", "Pilih lowongan aktif dari workspace yang sama.", nil)
		return
	}
	item := domain.Candidate{OrganizationID: id.OrganizationID, JobPostingID: req.JobPostingID, FullName: strings.TrimSpace(req.FullName), Email: strings.ToLower(strings.TrimSpace(req.Email)), Source: req.Source, Rating: req.Rating, Stage: "inbox"}
	if err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		history := domain.CandidateStageHistory{OrganizationID: id.OrganizationID, CandidateID: item.ID, ToStage: "inbox", ActorID: id.UserID}
		if err := tx.Create(&history).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "create", "candidate", item.ID, nil, item)
	}); err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}
func (s *Server) moveCandidate(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		Stage string `json:"stage" binding:"required,oneof=inbox screening interview offer hired rejected"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Tahap tidak valid", err.Error(), nil)
		return
	}
	var item domain.Candidate
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Where("organization_id=? AND id=?", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
			return err
		}
		before := item
		if item.Stage == req.Stage {
			return nil
		}
		if !recruitmentcalc.CanTransition(item.Stage, req.Stage) {
			return fmt.Errorf("invalid candidate stage transition from %s to %s", item.Stage, req.Stage)
		}
		history := domain.CandidateStageHistory{OrganizationID: id.OrganizationID, CandidateID: item.ID, FromStage: item.Stage, ToStage: req.Stage, ActorID: id.UserID}
		item.Stage = req.Stage
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if err := tx.Create(&history).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "stage_change", "candidate", item.ID, before, item)
	})
	if err != nil {
		if strings.Contains(err.Error(), "invalid candidate stage transition") {
			problem(c, http.StatusConflict, "invalid_candidate_stage", "Tahap kandidat tidak dapat diubah", err.Error(), nil)
		} else {
			databaseProblem(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) listPerformanceCycles(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.PerformanceCycle
	if err := s.db.Where("organization_id=?", id.OrganizationID).Order("start_date DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) createPerformanceCycle(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		Name      string `json:"name" binding:"required,max=160"`
		StartDate string `json:"startDate" binding:"required"`
		EndDate   string `json:"endDate" binding:"required"`
		Status    string `json:"status" binding:"required,oneof=draft active closed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Siklus tidak valid", err.Error(), nil)
		return
	}
	start, e1 := parseDate(req.StartDate)
	end, e2 := parseDate(req.EndDate)
	if e1 != nil || e2 != nil || end.Before(start) {
		problem(c, http.StatusBadRequest, "invalid_period", "Periode tidak valid", "Periksa tanggal siklus.", nil)
		return
	}
	item := domain.PerformanceCycle{OrganizationID: id.OrganizationID, Name: req.Name, StartDate: start, EndDate: end, Status: req.Status}
	if err := s.db.Create(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}
func (s *Server) listGoals(c *gin.Context) {
	id := identityFrom(c)
	employeeID := c.Query("employeeId")
	if employeeID == "" {
		employeeID = id.EmployeeID
	}
	if !s.employeeScopeAllowed(id, employeeID) {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Goal berada di luar cakupan Anda.", nil)
		return
	}
	var items []domain.Goal
	q := s.db.Preload("Cycle").Where("goals.organization_id=? AND goals.employee_id=?", id.OrganizationID, employeeID)
	if cycle := c.Query("cycleId"); cycle != "" {
		q = q.Where("goals.cycle_id=?", cycle)
	}
	if err := q.Order("created_at").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) createGoal(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		EmployeeID string `json:"employeeId"`
		CycleID    string `json:"cycleId" binding:"required,uuid"`
		Title      string `json:"title" binding:"required,max=240"`
		Category   string `json:"category"`
		Progress   int    `json:"progress" binding:"gte=0,lte=100"`
		Weight     int    `json:"weight" binding:"gte=0,lte=100"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Goal tidak valid", err.Error(), nil)
		return
	}
	if req.EmployeeID == "" {
		req.EmployeeID = id.EmployeeID
	}
	if !s.employeeScopeAllowed(id, req.EmployeeID) {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Karyawan berada di luar cakupan Anda.", nil)
		return
	}
	if !tenantReferenceExists(s.db, &domain.PerformanceCycle{}, id.OrganizationID, req.CycleID) {
		problem(c, http.StatusBadRequest, "invalid_cycle", "Siklus tidak valid", "Siklus harus berasal dari workspace yang sama.", nil)
		return
	}
	var current int
	s.db.Model(&domain.Goal{}).Where("organization_id=? AND employee_id=? AND cycle_id=?", id.OrganizationID, req.EmployeeID, req.CycleID).Select("COALESCE(SUM(weight),0)").Scan(&current)
	if err := performancecalc.ValidateGoal(req.Progress, req.Weight, current); err != nil {
		problem(c, http.StatusConflict, "invalid_goal_weight", "Bobot goal tidak valid", err.Error(), nil)
		return
	}
	item := domain.Goal{OrganizationID: id.OrganizationID, EmployeeID: req.EmployeeID, CycleID: req.CycleID, Title: req.Title, Category: req.Category, Progress: req.Progress, Weight: req.Weight}
	if err := s.db.Create(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}
func (s *Server) updateGoal(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		Progress *int    `json:"progress" binding:"omitempty,gte=0,lte=100"`
		Title    *string `json:"title"`
		Category *string `json:"category"`
		Weight   *int    `json:"weight" binding:"omitempty,gte=0,lte=100"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Goal tidak valid", err.Error(), nil)
		return
	}
	var item domain.Goal
	if err := s.db.Where("organization_id=? AND id=?", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if !s.employeeScopeAllowed(id, item.EmployeeID) {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Goal berada di luar cakupan Anda.", nil)
		return
	}
	updates := map[string]any{}
	if req.Progress != nil {
		updates["progress"] = *req.Progress
	}
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Category != nil {
		updates["category"] = *req.Category
	}
	if req.Weight != nil {
		var others int
		s.db.Model(&domain.Goal{}).Where("organization_id=? AND employee_id=? AND cycle_id=? AND id<>?", id.OrganizationID, item.EmployeeID, item.CycleID, item.ID).Select("COALESCE(SUM(weight),0)").Scan(&others)
		progress := item.Progress
		if req.Progress != nil {
			progress = *req.Progress
		}
		if err := performancecalc.ValidateGoal(progress, *req.Weight, others); err != nil {
			problem(c, http.StatusConflict, "invalid_goal_weight", "Bobot goal tidak valid", err.Error(), nil)
			return
		}
		updates["weight"] = *req.Weight
	}
	if err := s.db.Model(&item).Updates(updates).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := s.db.First(&item, "id=?", item.ID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
func (s *Server) listReviews(c *gin.Context) {
	id := identityFrom(c)
	employeeID := c.Query("employeeId")
	if employeeID == "" {
		employeeID = id.EmployeeID
	}
	if !s.employeeScopeAllowed(id, employeeID) {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Review berada di luar cakupan Anda.", nil)
		return
	}
	var items []domain.Review
	q := s.db.Preload("Reviewer").Preload("Cycle").Where("reviews.organization_id=? AND reviews.employee_id=?", id.OrganizationID, employeeID)
	if cycle := c.Query("cycleId"); cycle != "" {
		q = q.Where("reviews.cycle_id=?", cycle)
	}
	if err := q.Order("created_at DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) createReview(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		CycleID    string `json:"cycleId" binding:"required,uuid"`
		EmployeeID string `json:"employeeId" binding:"required,uuid"`
		Rating     int    `json:"rating" binding:"required,gte=1,lte=5"`
		Relation   string `json:"relation"`
		Notes      string `json:"notes" binding:"required,max=4000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Review tidak valid", err.Error(), nil)
		return
	}
	if id.EmployeeID == "" {
		problem(c, http.StatusConflict, "employee_not_linked", "Profil reviewer belum terhubung", "Hubungi HR Admin.", nil)
		return
	}
	if !s.employeeScopeAllowed(id, req.EmployeeID) || !tenantReferenceExists(s.db, &domain.PerformanceCycle{}, id.OrganizationID, req.CycleID) {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Karyawan atau siklus berada di luar cakupan Anda.", nil)
		return
	}
	item := domain.Review{OrganizationID: id.OrganizationID, CycleID: req.CycleID, EmployeeID: req.EmployeeID, ReviewerID: id.EmployeeID, Rating: req.Rating, Relation: req.Relation, Notes: req.Notes, CreatedAt: time.Now().UTC()}
	if err := s.db.Create(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}
