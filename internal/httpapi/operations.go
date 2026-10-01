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
	attendancecalc "github.com/simpul/hr-backend/internal/modules/attendance"
	leavecalc "github.com/simpul/hr-backend/internal/modules/leave"
	"github.com/simpul/hr-backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) registerOperationsRoutes(r *gin.RouterGroup) {
	r.GET("/attendance", permissionRequired("attendance.view"), s.listAttendance)
	r.GET("/attendance/export", permissionRequired("attendance.view"), s.exportAttendance)
	r.POST("/attendance", permissionRequired("attendance.manage"), s.manualAttendance)
	r.PATCH("/attendance/:id", permissionRequired("attendance.manage"), s.updateAttendance)
	r.POST("/attendance/clock-in", s.clockIn)
	r.POST("/attendance/clock-out", s.clockOut)
	r.GET("/shift-assignments", permissionRequired("attendance.view"), s.listShifts)
	r.PUT("/shift-assignments", permissionRequired("attendance.manage"), s.assignShift)
	r.GET("/leave-types", s.listLeaveTypes)
	r.GET("/leave-balances", s.leaveBalances)
	r.GET("/leave-requests", s.listLeaveRequests)
	r.POST("/leave-requests", s.createLeaveRequest)
	r.POST("/leave-requests/:id/approve", permissionRequired("leave.approve"), s.approveLeave)
	r.POST("/leave-requests/:id/reject", permissionRequired("leave.approve"), s.rejectLeave)
	r.GET("/payroll-policies", permissionRequired("payroll.view"), s.listPayrollPolicies)
	r.GET("/payroll-runs", permissionRequired("payroll.view"), s.listPayrollRuns)
	r.POST("/payroll-runs", permissionRequired("payroll.run"), s.createPayrollRun)
	r.GET("/payroll-runs/:id", permissionRequired("payroll.view"), s.getPayrollRun)
	r.POST("/payroll-runs/:id/execute", permissionRequired("payroll.run"), s.executePayroll)
	r.GET("/payslips", s.listPayslips)
	// Same permission as the list view, but the handler additionally enforces employee
	// scope and answers 403 for an out-of-scope `employeeId` instead of an empty file.
	r.GET("/payslips/export", s.exportPayslips)
}

func (s *Server) organizationLocation(organizationID string) *time.Location {
	var timezone string
	if err := s.db.Model(&domain.Organization{}).Where("id = ?", organizationID).Pluck("timezone", &timezone).Error; err == nil && timezone != "" {
		if location, err := time.LoadLocation(timezone); err == nil {
			return location
		}
	}
	location, _ := time.LoadLocation("Asia/Jakarta")
	return location
}

// attendanceQuery applies the tenant scoping and filters shared by the paginated list and
// the CSV export, so an export can never drift from what the table displays.
func (s *Server) attendanceQuery(c *gin.Context, id Identity) *gorm.DB {
	q := s.db.Model(&domain.Attendance{}).Where("attendances.organization_id = ?", id.OrganizationID)
	if id.Role == "employee" && id.EmployeeID != "" {
		q = q.Where("attendances.employee_id = ?", id.EmployeeID)
	} else if id.Role == "team_lead" && id.EmployeeID != "" {
		q = q.Joins("JOIN employees scoped_employee ON scoped_employee.id=attendances.employee_id").Where("scoped_employee.manager_id=? OR scoped_employee.id=?", id.EmployeeID, id.EmployeeID)
	}
	if date := c.Query("date"); date != "" {
		q = q.Where("attendances.date = ?", date)
	}
	if from := c.Query("from"); from != "" {
		q = q.Where("attendances.date >= ?", from)
	}
	if to := c.Query("to"); to != "" {
		q = q.Where("attendances.date <= ?", to)
	}
	if dept := c.Query("departmentId"); dept != "" {
		q = q.Joins("JOIN employees department_employee ON department_employee.id=attendances.employee_id").Where("department_employee.department_id=?", dept)
	}
	return q
}

func (s *Server) listAttendance(c *gin.Context) {
	id := identityFrom(c)
	page, size := pagination(c)
	var items []domain.Attendance
	var total int64
	q := s.attendanceQuery(c, id)
	if err := q.Count(&total).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := q.Preload("Employee.Department").Preload("Employee.Position").Order("date DESC, created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}})
}

// exportAttendance streams every attendance row matching the current filters.
func (s *Server) exportAttendance(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Attendance
	if err := s.attendanceQuery(c, id).
		Preload("Employee.Department").
		Order("date DESC, created_at DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			formatCSVDate(item.Date),
			item.Employee.FullName,
			item.Employee.EmployeeCode,
			item.Employee.Department.Name,
			formatCSVTime(item.ClockIn),
			formatCSVTime(item.ClockOut),
			strconv.Itoa(item.TotalMinutes),
			strconv.Itoa(item.OvertimeMinutes),
			item.Status,
			item.Source,
			item.Notes,
		})
	}
	writeCSV(c, "kehadiran.csv",
		[]string{"Tanggal", "Nama", "Kode Karyawan", "Departemen", "Masuk", "Keluar", "Total Menit", "Lembur (menit)", "Status", "Sumber", "Catatan"},
		rows)
}

type attendanceRequest struct {
	EmployeeID string `json:"employeeId" binding:"required,uuid"`
	Date       string `json:"date" binding:"required"`
	ClockIn    string `json:"clockIn" binding:"required"`
	ClockOut   string `json:"clockOut" binding:"required"`
	Notes      string `json:"notes"`
}

func (s *Server) manualAttendance(c *gin.Context) {
	id := identityFrom(c)
	var req attendanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Absensi tidak valid", err.Error(), nil)
		return
	}
	if !tenantReferenceExists(s.db, &domain.Employee{}, id.OrganizationID, req.EmployeeID) {
		problem(c, http.StatusBadRequest, "invalid_employee", "Karyawan tidak valid", "Karyawan harus berasal dari workspace yang sama.", nil)
		return
	}
	date, err := parseDate(req.Date)
	if err != nil {
		problem(c, http.StatusBadRequest, "invalid_date", "Tanggal tidak valid", "Gunakan YYYY-MM-DD.", nil)
		return
	}
	location := s.organizationLocation(id.OrganizationID)
	clockIn, err := time.ParseInLocation("2006-01-02 15:04", req.Date+" "+req.ClockIn, location)
	if err != nil {
		problem(c, http.StatusBadRequest, "invalid_clock_in", "Jam masuk tidak valid", "Gunakan HH:mm.", nil)
		return
	}
	clockOut, err := time.ParseInLocation("2006-01-02 15:04", req.Date+" "+req.ClockOut, location)
	if err != nil {
		problem(c, http.StatusBadRequest, "invalid_clock_out", "Jam keluar tidak valid", "Gunakan HH:mm.", nil)
		return
	}
	if !clockOut.After(clockIn) {
		clockOut = clockOut.Add(24 * time.Hour)
	}
	total, overtime := attendancecalc.Duration(clockIn, clockOut)
	item := domain.Attendance{OrganizationID: id.OrganizationID, EmployeeID: req.EmployeeID, Date: date, ClockIn: &clockIn, ClockOut: &clockOut, TotalMinutes: total, OvertimeMinutes: overtime, Status: attendancecalc.StatusFor(clockIn, 9, 0), Source: "manual", Notes: req.Notes}
	err = s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		var existing domain.Attendance
		lookup := tx.Where("organization_id=? AND employee_id=? AND date=?", id.OrganizationID, req.EmployeeID, date).First(&existing).Error
		if lookup == nil {
			item.ID = existing.ID
			updates := map[string]any{
				"clock_in":         clockIn,
				"clock_out":        clockOut,
				"total_minutes":    total,
				"overtime_minutes": overtime,
				"status":           item.Status,
				"source":           "manual",
				"notes":            req.Notes,
			}
			if err := tx.Model(&existing).Updates(updates).Error; err != nil {
				return err
			}
			return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "update", "attendance", existing.ID, existing, item)
		}
		if lookup != gorm.ErrRecordNotFound {
			return lookup
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "create", "attendance", item.ID, nil, item)
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (s *Server) updateAttendance(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		ClockIn  string `json:"clockIn" binding:"required"`
		ClockOut string `json:"clockOut" binding:"required"`
		Notes    string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Absensi tidak valid", err.Error(), nil)
		return
	}
	var item domain.Attendance
	if err := s.db.Where("organization_id=? AND id=?", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	dateText := item.Date.Format("2006-01-02")
	location := s.organizationLocation(id.OrganizationID)
	clockIn, e1 := time.ParseInLocation("2006-01-02 15:04", dateText+" "+req.ClockIn, location)
	clockOut, e2 := time.ParseInLocation("2006-01-02 15:04", dateText+" "+req.ClockOut, location)
	if e1 != nil || e2 != nil {
		problem(c, http.StatusBadRequest, "invalid_time", "Jam tidak valid", "Gunakan HH:mm.", nil)
		return
	}
	if !clockOut.After(clockIn) {
		clockOut = clockOut.Add(24 * time.Hour)
	}
	total, overtime := attendancecalc.Duration(clockIn, clockOut)
	before := item
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Model(&item).Updates(map[string]any{"clock_in": clockIn, "clock_out": clockOut, "total_minutes": total, "overtime_minutes": overtime, "notes": req.Notes, "source": "manual", "status": attendancecalc.StatusFor(clockIn, 9, 0)}).Error; err != nil {
			return err
		}
		if err := tx.First(&item, "id=?", item.ID).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "update", "attendance", item.ID, before, item)
	})
	if err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) clockIn(c *gin.Context) {
	id := identityFrom(c)
	if id.EmployeeID == "" {
		problem(c, http.StatusConflict, "employee_not_linked", "Profil karyawan belum terhubung", "Hubungi HR Admin.", nil)
		return
	}
	location := s.organizationLocation(id.OrganizationID)
	now := time.Now().In(location)
	date, _ := parseDate(now.Format("2006-01-02"))
	item := domain.Attendance{OrganizationID: id.OrganizationID, EmployeeID: id.EmployeeID, Date: date, ClockIn: &now, Status: attendancecalc.StatusFor(now, 9, 0), Source: "self"}
	result := s.db.Where("organization_id=? AND employee_id=? AND date=?", id.OrganizationID, id.EmployeeID, date).FirstOrCreate(&item)
	if result.Error != nil {
		databaseProblem(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		if item.ClockIn != nil {
			problem(c, http.StatusConflict, "already_clocked_in", "Sudah clock-in", "Clock-in hari ini telah tercatat.", nil)
			return
		}
		status := attendancecalc.StatusFor(now, 9, 0)
		if err := s.db.Model(&item).Updates(map[string]any{"clock_in": now, "status": status, "source": "self"}).Error; err != nil {
			databaseProblem(c, err)
			return
		}
		item.ClockIn = &now
		item.Status = status
		item.Source = "self"
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) clockOut(c *gin.Context) {
	id := identityFrom(c)
	if id.EmployeeID == "" {
		problem(c, http.StatusConflict, "employee_not_linked", "Profil karyawan belum terhubung", "Hubungi HR Admin.", nil)
		return
	}
	location := s.organizationLocation(id.OrganizationID)
	now := time.Now().In(location)
	date, _ := parseDate(now.Format("2006-01-02"))
	var item domain.Attendance
	if err := s.db.Where("organization_id=? AND employee_id=? AND date=?", id.OrganizationID, id.EmployeeID, date).First(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if item.ClockIn == nil {
		problem(c, http.StatusConflict, "clock_in_required", "Belum clock-in", "Catat jam masuk terlebih dahulu.", nil)
		return
	}
	if item.ClockOut != nil {
		problem(c, http.StatusConflict, "already_clocked_out", "Sudah clock-out", "Clock-out hari ini telah tercatat.", nil)
		return
	}
	total, overtime := attendancecalc.Duration(*item.ClockIn, now)
	if err := s.db.Model(&item).Updates(map[string]any{"clock_out": now, "total_minutes": total, "overtime_minutes": overtime}).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := s.db.First(&item, "id=?", item.ID).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	item.ClockOut = &now
	item.TotalMinutes = total
	item.OvertimeMinutes = overtime
	c.JSON(http.StatusOK, item)
}

func (s *Server) listShifts(c *gin.Context) {
	id := identityFrom(c)
	from := c.Query("from")
	if from == "" {
		from = time.Now().Format("2006-01-02")
	}
	to := c.Query("to")
	if to == "" {
		start, _ := parseDate(from)
		to = start.AddDate(0, 0, 6).Format("2006-01-02")
	}
	var items []domain.ShiftAssignment
	q := s.db.Preload("Employee.Department").Where("shift_assignments.organization_id=? AND date BETWEEN ? AND ?", id.OrganizationID, from, to)
	if id.Role == "employee" {
		q = q.Where("employee_id=?", id.EmployeeID)
	}
	if err := q.Order("date, employee_id").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "from": from, "to": to})
}
func (s *Server) assignShift(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		EmployeeID string `json:"employeeId" binding:"required,uuid"`
		Date       string `json:"date" binding:"required"`
		ShiftType  string `json:"shiftType" binding:"required,oneof=morning afternoon night off"`
		StartTime  string `json:"startTime"`
		EndTime    string `json:"endTime"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Shift tidak valid", err.Error(), nil)
		return
	}
	if !tenantReferenceExists(s.db, &domain.Employee{}, id.OrganizationID, req.EmployeeID) {
		problem(c, http.StatusBadRequest, "invalid_employee", "Karyawan tidak valid", "Karyawan harus berasal dari workspace yang sama.", nil)
		return
	}
	date, err := parseDate(req.Date)
	if err != nil {
		problem(c, http.StatusBadRequest, "invalid_date", "Tanggal tidak valid", "Gunakan YYYY-MM-DD.", nil)
		return
	}
	item := domain.ShiftAssignment{OrganizationID: id.OrganizationID, EmployeeID: req.EmployeeID, Date: date, ShiftType: req.ShiftType, StartTime: req.StartTime, EndTime: req.EndTime}
	if err := s.db.Where("organization_id=? AND employee_id=? AND date=?", id.OrganizationID, req.EmployeeID, date).Assign(item).FirstOrCreate(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) listLeaveTypes(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.LeaveType
	if err := s.db.Where("organization_id=?", id.OrganizationID).Order("name").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) leaveBalances(c *gin.Context) {
	id := identityFrom(c)
	employeeID := c.Query("employeeId")
	if employeeID == "" {
		employeeID = id.EmployeeID
	}
	if !s.employeeScopeAllowed(id, employeeID) {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Saldo berada di luar cakupan Anda.", nil)
		return
	}
	year := intQuery(c, "year", time.Now().Year())
	var items []domain.LeaveBalance
	if err := s.db.Preload("LeaveType").Where("organization_id=? AND employee_id=? AND year=?", id.OrganizationID, employeeID, year).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) listLeaveRequests(c *gin.Context) {
	id := identityFrom(c)
	page, size := pagination(c)
	var items []domain.LeaveRequest
	var total int64
	q := s.db.Model(&domain.LeaveRequest{}).Where("leave_requests.organization_id=?", id.OrganizationID)
	scope := c.DefaultQuery("scope", "mine")
	if scope == "mine" || id.Role == "employee" {
		q = q.Where("leave_requests.employee_id=?", id.EmployeeID)
	} else if id.Role == "team_lead" {
		q = q.Joins("JOIN employees scope_employee ON scope_employee.id=leave_requests.employee_id").Where("scope_employee.manager_id=?", id.EmployeeID)
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("leave_requests.status=?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	if err := q.Preload("Employee").Preload("LeaveType").Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}})
}

func (s *Server) createLeaveRequest(c *gin.Context) {
	id := identityFrom(c)
	if id.EmployeeID == "" {
		problem(c, http.StatusConflict, "employee_not_linked", "Profil karyawan belum terhubung", "Hubungi HR Admin.", nil)
		return
	}
	var req struct {
		LeaveTypeID string `json:"leaveTypeId" binding:"required,uuid"`
		StartDate   string `json:"startDate" binding:"required"`
		EndDate     string `json:"endDate" binding:"required"`
		Reason      string `json:"reason" binding:"required,min=3,max=1000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Pengajuan tidak valid", err.Error(), nil)
		return
	}
	start, err := parseDate(req.StartDate)
	if err != nil {
		problem(c, http.StatusBadRequest, "invalid_date", "Tanggal tidak valid", "Gunakan YYYY-MM-DD.", nil)
		return
	}
	end, err := parseDate(req.EndDate)
	if err != nil || end.Before(start) {
		problem(c, http.StatusBadRequest, "invalid_date_range", "Rentang tanggal tidak valid", "Tanggal selesai harus setelah tanggal mulai.", nil)
		return
	}
	var holidays []domain.Holiday
	s.db.Where("organization_id=? AND date BETWEEN ? AND ?", id.OrganizationID, start, end).Find(&holidays)
	holidayMap := map[string]bool{}
	for _, holiday := range holidays {
		holidayMap[holiday.Date.Format("2006-01-02")] = true
	}
	duration := leavecalc.WorkingDays(start, end, holidayMap)
	if duration == 0 {
		problem(c, http.StatusBadRequest, "no_working_days", "Tidak ada hari kerja", "Rentang hanya berisi akhir pekan atau hari libur.", nil)
		return
	}
	var overlap int64
	s.db.Model(&domain.LeaveRequest{}).Where("organization_id=? AND employee_id=? AND status IN ('pending','approved') AND start_date<=? AND end_date>=?", id.OrganizationID, id.EmployeeID, end, start).Count(&overlap)
	if overlap > 0 {
		problem(c, http.StatusConflict, "leave_overlap", "Tanggal bertumpang tindih", "Sudah ada pengajuan pada rentang tersebut.", nil)
		return
	}
	var balance domain.LeaveBalance
	if err := s.db.Where("organization_id=? AND employee_id=? AND leave_type_id=? AND year=?", id.OrganizationID, id.EmployeeID, req.LeaveTypeID, start.Year()).First(&balance).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	var pendingDays int64
	s.db.Model(&domain.LeaveRequest{}).Where("organization_id=? AND employee_id=? AND leave_type_id=? AND status='pending' AND EXTRACT(YEAR FROM start_date)=?", id.OrganizationID, id.EmployeeID, req.LeaveTypeID, start.Year()).Select("COALESCE(SUM(duration_days), 0)").Scan(&pendingDays)
	available := balance.Quota - balance.Taken - int(pendingDays)
	if available < duration {
		problem(c, http.StatusConflict, "insufficient_leave_balance", "Saldo cuti tidak cukup", fmt.Sprintf("Tersedia %d hari (setelah memperhitungkan pengajuan pending), diminta %d hari.", available, duration), nil)
		return
	}
	item := domain.LeaveRequest{OrganizationID: id.OrganizationID, EmployeeID: id.EmployeeID, LeaveTypeID: req.LeaveTypeID, StartDate: start, EndDate: end, DurationDays: duration, Reason: req.Reason, Status: "pending"}
	if err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		if err := repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "create", "leave_request", item.ID, nil, item); err != nil {
			return err
		}
		return repository.AddOutbox(tx, id.OrganizationID, "notification:digest", gin.H{"organizationId": id.OrganizationID, "leaveRequestId": item.ID})
	}); err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (s *Server) approveLeave(c *gin.Context) { s.decideLeave(c, "approved") }
func (s *Server) rejectLeave(c *gin.Context)  { s.decideLeave(c, "rejected") }
func (s *Server) decideLeave(c *gin.Context, status string) {
	id := identityFrom(c)
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	var item domain.LeaveRequest
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id=? AND id=?", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
			return err
		}
		if item.Status != "pending" {
			return fmt.Errorf("leave request is already %s", item.Status)
		}
		if id.Role != "hr_admin" {
			var directReport int64
			if id.EmployeeID != "" {
				tx.Model(&domain.Employee{}).Where("organization_id=? AND id=? AND manager_id=?", id.OrganizationID, item.EmployeeID, id.EmployeeID).Count(&directReport)
			}
			if directReport == 0 {
				return fmt.Errorf("scope denied for leave request")
			}
		}
		before := item
		now := time.Now().UTC()
		item.Status = status
		item.DecisionNote = req.Note
		item.DecidedAt = &now
		if id.EmployeeID != "" {
			item.ApproverID = &id.EmployeeID
		}
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if status == "approved" {
			var balance domain.LeaveBalance
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id=? AND employee_id=? AND leave_type_id=? AND year=?", id.OrganizationID, item.EmployeeID, item.LeaveTypeID, item.StartDate.Year()).First(&balance).Error; err != nil {
				return err
			}
			if balance.Quota-balance.Taken < item.DurationDays {
				return fmt.Errorf("insufficient leave balance during approval")
			}
			if err := tx.Model(&balance).Update("taken", balance.Taken+item.DurationDays).Error; err != nil {
				return err
			}
		}
		if err := repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), status, "leave_request", item.ID, before, item); err != nil {
			return err
		}
		return repository.AddOutbox(tx, id.OrganizationID, "notification:digest", gin.H{"organizationId": id.OrganizationID, "leaveRequestId": item.ID, "status": status})
	})
	if err != nil {
		if strings.Contains(err.Error(), "already") {
			problem(c, http.StatusConflict, "invalid_leave_state", "Status tidak dapat diubah", err.Error(), nil)
		} else if strings.Contains(err.Error(), "scope denied") {
			problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Pengajuan bukan milik direct report Anda.", nil)
		} else if strings.Contains(err.Error(), "insufficient leave balance") {
			problem(c, http.StatusConflict, "insufficient_leave_balance", "Saldo cuti tidak cukup", "Saldo berubah sejak pengajuan dibuat.", nil)
		} else {
			databaseProblem(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) listPayrollPolicies(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.PayrollPolicy
	if err := s.db.Where("organization_id=?", id.OrganizationID).Order("effective_from DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}
func (s *Server) listPayrollRuns(c *gin.Context) {
	id := identityFrom(c)
	page, size := pagination(c)
	var items []domain.PayrollRun
	var total int64
	q := s.db.Model(&domain.PayrollRun{}).Where("organization_id=?", id.OrganizationID)
	q.Count(&total)
	if err := q.Order("period_end DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}})
}
func (s *Server) createPayrollRun(c *gin.Context) {
	id := identityFrom(c)
	var req struct {
		PolicyID    string `json:"policyId" binding:"required,uuid"`
		PeriodStart string `json:"periodStart" binding:"required"`
		PeriodEnd   string `json:"periodEnd" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		problem(c, http.StatusBadRequest, "validation_failed", "Payroll run tidak valid", err.Error(), nil)
		return
	}
	start, e1 := parseDate(req.PeriodStart)
	end, e2 := parseDate(req.PeriodEnd)
	if e1 != nil || e2 != nil || end.Before(start) {
		problem(c, http.StatusBadRequest, "invalid_period", "Periode tidak valid", "Periksa tanggal mulai dan selesai.", nil)
		return
	}
	var policyCount int64
	s.db.Model(&domain.PayrollPolicy{}).Where("organization_id=? AND id=? AND effective_from<=? AND (effective_to IS NULL OR effective_to>=?)", id.OrganizationID, req.PolicyID, end, start).Count(&policyCount)
	if policyCount != 1 {
		problem(c, http.StatusBadRequest, "invalid_payroll_policy", "Policy payroll tidak valid", "Pilih policy workspace yang efektif untuk seluruh periode.", nil)
		return
	}
	item := domain.PayrollRun{OrganizationID: id.OrganizationID, PolicyID: req.PolicyID, PeriodStart: start, PeriodEnd: end, Status: "draft"}
	if err := s.db.Create(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}
func (s *Server) getPayrollRun(c *gin.Context) {
	id := identityFrom(c)
	var item domain.PayrollRun
	if err := s.db.Preload("Payslips.Employee").Where("organization_id=? AND id=?", id.OrganizationID, c.Param("id")).First(&item).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) executePayroll(c *gin.Context) {
	id := identityFrom(c)
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		problem(c, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key wajib", "Gunakan UUID unik untuk setiap aksi execute.", nil)
		return
	}
	if len(key) > 128 {
		problem(c, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key tidak valid", "Panjang maksimum 128 karakter.", nil)
		return
	}
	var response gin.H
	err := s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		var existing domain.IdempotencyKey
		if err := tx.Where("organization_id=? AND scope=? AND key=?", id.OrganizationID, "payroll.execute", key).First(&existing).Error; err == nil {
			response = gin.H{"payrollRunId": existing.ResourceID, "status": "queued", "idempotentReplay": true}
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		var run domain.PayrollRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("organization_id=? AND id=?", id.OrganizationID, c.Param("id")).First(&run).Error; err != nil {
			return err
		}
		if run.Status != "draft" && run.Status != "queued" {
			return fmt.Errorf("payroll run cannot execute from %s", run.Status)
		}
		if run.Status == "draft" {
			if err := tx.Model(&run).Update("status", "queued").Error; err != nil {
				return err
			}
		}
		payload := gin.H{"organizationId": id.OrganizationID, "payrollRunId": run.ID}
		if err := repository.AddOutbox(tx, id.OrganizationID, "payroll:process", payload); err != nil {
			return err
		}
		response = gin.H{"payrollRunId": run.ID, "status": "queued", "idempotentReplay": false}
		raw, _ := json.Marshal(response)
		record := domain.IdempotencyKey{OrganizationID: id.OrganizationID, Scope: "payroll.execute", Key: key, ResourceID: run.ID, Response: raw, ExpiresAt: time.Now().UTC().Add(48 * time.Hour)}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "execute", "payroll_run", run.ID, nil, response)
	})
	if err != nil {
		if strings.Contains(err.Error(), "cannot execute") {
			problem(c, http.StatusConflict, "invalid_payroll_state", "Payroll tidak dapat dijalankan", err.Error(), nil)
		} else {
			databaseProblem(c, err)
		}
		return
	}
	c.JSON(http.StatusAccepted, response)
}

// payslipQuery applies the tenant scoping and filters shared by the paginated list and the
// CSV export. The boolean is false when the caller requested an employee outside their
// scope; callers must translate that into a 403 rather than an empty result.
func (s *Server) payslipQuery(c *gin.Context, id Identity) (*gorm.DB, bool) {
	q := s.db.Model(&domain.Payslip{}).Where("payslips.organization_id=?", id.OrganizationID)
	employeeID := c.Query("employeeId")
	if id.Role == "employee" {
		// Karyawan hanya boleh melihat slip gajinya sendiri. Permintaan atas employeeId orang
		// lain dijawab 403 secara eksplisit: diam-diam mengembalikan data milik sendiri akan
		// membuat pemanggil mengira permintaannya terpenuhi, dan ekspor akan berisi orang yang
		// salah tanpa peringatan apa pun.
		if employeeID != "" && employeeID != id.EmployeeID {
			return nil, false
		}
		q = q.Where("payslips.employee_id=?", id.EmployeeID)
	} else if employeeID != "" {
		if !s.employeeScopeAllowed(id, employeeID) {
			return nil, false
		}
		q = q.Where("payslips.employee_id=?", employeeID)
	}
	if runID := c.Query("payrollRunId"); runID != "" {
		q = q.Where("payslips.payroll_run_id=?", runID)
	}
	return q, true
}

func (s *Server) listPayslips(c *gin.Context) {
	id := identityFrom(c)
	page, size := pagination(c)
	var items []domain.Payslip
	var total int64
	q, allowed := s.payslipQuery(c, id)
	if !allowed {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Slip gaji berada di luar cakupan Anda.", nil)
		return
	}
	q.Count(&total)
	// The department and position are preloaded for the same reason the attendance register
	// does it: without them the nested objects still serialise, as empty structs with an empty
	// id and a zero timestamp, so a client that reads `payslip.employee.department.name` gets
	// "" rather than an absent field it could have handled explicitly.
	if err := q.Preload("Employee.Department").Preload("Employee.Position").Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": listMeta{Page: page, PageSize: size, Total: total}})
}

// exportPayslips streams every payslip matching the current filters. Amounts are written as
// plain integers (whole rupiah) so a spreadsheet can sum them without parsing separators.
func (s *Server) exportPayslips(c *gin.Context) {
	id := identityFrom(c)
	var items []domain.Payslip
	q, allowed := s.payslipQuery(c, id)
	if !allowed {
		problem(c, http.StatusForbidden, "scope_denied", "Akses ditolak", "Slip gaji berada di luar cakupan Anda.", nil)
		return
	}
	if err := q.Preload("Employee").Order("created_at DESC").Find(&items).Error; err != nil {
		databaseProblem(c, err)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.Employee.FullName,
			item.Employee.EmployeeCode,
			strconv.FormatInt(item.GrossPay, 10),
			strconv.FormatInt(item.BPJSDeduction, 10),
			strconv.FormatInt(item.PPh21Deduction, 10),
			strconv.FormatInt(item.OtherDeduction, 10),
			strconv.FormatInt(item.NetPay, 10),
		})
	}
	writeCSV(c, "slip-gaji.csv",
		[]string{"Nama", "Kode Karyawan", "Bruto", "BPJS", "PPh 21", "Potongan Lain", "Neto"},
		rows)
}
