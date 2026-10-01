package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/simpul/hr-backend/internal/domain"
	"github.com/simpul/hr-backend/internal/modules/payroll"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// PlaceholderPassword is the value shipped in .env.example.
//
// It is published in this repository, so accepting it anywhere outside development hands
// anyone who reads the source a working HR administrator account. That matters more here
// than in most projects: this one is intended to be public.
const PlaceholderPassword = "ChangeMe-OnlyForLocalDevelopment-123!"

// minPasswordLength is a floor for the seeded accounts. They are not throwaway users — the
// seeded administrator holds every permission in the workspace.
const minPasswordLength = 12

// Validate rejects the seed passwords that are safe to publish and unsafe to use.
//
// It deliberately does NOT refuse to run in production. Seeding the demo workspace into a
// production-configured deployment is the intended setup for this project, which exists to be
// demonstrated — so the thing worth guarding is a *known* password, not the act of seeding.
func Validate(environment, adminPassword string) error {
	if strings.TrimSpace(adminPassword) == "" {
		return fmt.Errorf("SEED_ADMIN_PASSWORD is required")
	}
	if adminPassword == PlaceholderPassword && environment != "development" && environment != "test" {
		return fmt.Errorf("SEED_ADMIN_PASSWORD is still the example value from .env.example, which is published in this repository; set a real password before seeding with APP_ENV=%s", environment)
	}
	if len([]rune(adminPassword)) < minPasswordLength {
		return fmt.Errorf("SEED_ADMIN_PASSWORD must be at least %d characters, because it becomes an account holding every permission in the workspace", minPasswordLength)
	}
	return nil
}

type seededEmployee struct {
	Employee     domain.Employee
	Compensation domain.Compensation
}

func Run(ctx context.Context, db *gorm.DB, adminPassword string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		organization := domain.Organization{Slug: "simpul-demo"}
		if err := tx.Where("slug=?", organization.Slug).Assign(domain.Organization{Name: "PT Simpul Teknologi Indonesia", Industry: "Teknologi Informasi & SaaS", Address: "Sopo Del Tower Lt. 18, Mega Kuningan, Jakarta Selatan", Phone: "+62 21-5088-0123", Email: "ops@simpul.co.id", Timezone: "Asia/Jakarta", Locale: "id-ID", Currency: "IDR"}).FirstOrCreate(&organization).Error; err != nil {
			return err
		}
		permissionKeys := map[string]string{
			"employees.view": "Melihat Direktori Karyawan", "employees.manage": "Mengelola Data Karyawan",
			"attendance.view": "Melihat Absensi dan Shift", "attendance.manage": "Mengelola Absensi dan Shift",
			"leave.approve": "Menyetujui Cuti", "payroll.view": "Melihat Payroll", "payroll.run": "Menjalankan Payroll",
			"recruitment.view": "Melihat Rekrutmen", "recruitment.manage": "Mengelola Rekrutmen",
			"performance.view": "Melihat Performa", "performance.manage": "Mengelola Performa",
			"analytics.view": "Melihat Analitik", "settings.manage": "Mengelola Pengaturan", "support.manage": "Mengelola Tiket Bantuan",
		}
		permissions := map[string]domain.Permission{}
		for key, label := range permissionKeys {
			item := domain.Permission{Key: key}
			if err := tx.Where("key=?", key).Assign(domain.Permission{Label: label}).FirstOrCreate(&item).Error; err != nil {
				return err
			}
			permissions[key] = item
		}
		roleSets := map[string][]string{
			"hr_admin":        keys(permissionKeys),
			"finance_manager": {"employees.view", "payroll.view", "payroll.run", "analytics.view"},
			"team_lead":       {"employees.view", "attendance.view", "leave.approve", "recruitment.view", "performance.view", "performance.manage", "analytics.view"},
			"employee":        {"employees.view", "attendance.view", "performance.view"},
		}
		roleNames := map[string]string{"hr_admin": "HR Admin", "finance_manager": "Finance Manager", "team_lead": "Team Lead / Manager", "employee": "Employee (Self-Service)"}
		roles := map[string]domain.Role{}
		for key, set := range roleSets {
			role := domain.Role{OrganizationID: organization.ID, Key: key}
			if err := tx.Where("organization_id=? AND key=?", organization.ID, key).Assign(domain.Role{Name: roleNames[key], Description: "System role " + key, IsSystem: true}).FirstOrCreate(&role).Error; err != nil {
				return err
			}
			assigned := make([]domain.Permission, 0, len(set))
			for _, permissionKey := range set {
				assigned = append(assigned, permissions[permissionKey])
			}
			if err := tx.Model(&role).Association("Permissions").Replace(assigned); err != nil {
				return err
			}
			roles[key] = role
		}

		departmentNames := []string{"Design", "Engineering", "Marketing", "People (HR)"}
		departments := map[string]domain.Department{}
		for _, name := range departmentNames {
			item := domain.Department{OrganizationID: organization.ID, Name: name}
			if err := tx.Where("organization_id=? AND name=?", organization.ID, name).FirstOrCreate(&item).Error; err != nil {
				return err
			}
			departments[name] = item
		}
		positionSpecs := []struct{ Title, Department, Level string }{{"Chief Executive Officer (CEO)", "People (HR)", "Executive"}, {"HR Admin / People Ops", "People (HR)", "Lead"}, {"Product Designer", "Design", "Individual Contributor"}, {"Senior Backend Engineer", "Engineering", "Senior"}, {"Engineering Manager", "Engineering", "Manager"}, {"Social Media Specialist", "Marketing", "Individual Contributor"}}
		positions := map[string]domain.Position{}
		for _, spec := range positionSpecs {
			item := domain.Position{OrganizationID: organization.ID, DepartmentID: departments[spec.Department].ID, Title: spec.Title}
			if err := tx.Where("organization_id=? AND department_id=? AND title=?", organization.ID, item.DepartmentID, item.Title).Assign(domain.Position{Level: spec.Level}).FirstOrCreate(&item).Error; err != nil {
				return err
			}
			positions[spec.Title] = item
		}

		now := time.Now().In(mustLocation("Asia/Jakarta"))
		employees := map[string]seededEmployee{}
		employeeSpecs := []struct {
			Code, Name, Email, Phone, Department, Position, Status string
			Salary                                                 int64
			Gender                                                 string
			HireDate                                               time.Time
		}{
			{"EMP-0000", "Dewi Lestari", "dewi@simpul.co.id", "+62 812-0000-0000", "People (HR)", "Chief Executive Officer (CEO)", "active", 40_000_000, "female", date(2022, 1, 3)},
			{"EMP-0001", "Sarah", "sarah@simpul.co.id", "+62 812-1122-3344", "People (HR)", "HR Admin / People Ops", "active", 22_000_000, "female", date(2023, 1, 1)},
			{"EMP-0214", "Rani Wijaya", "rani.wijaya@simpul.co.id", "+62 812-3456-7890", "Design", "Product Designer", "active", 12_500_000, "female", date(2024, 1, 15)},
			{"EMP-0215", "Aditya Pratama", "aditya.p@simpul.co.id", "+62 813-9876-5432", "Engineering", "Senior Backend Engineer", "active", 18_000_000, "male", date(2024, 2, 10)},
			{"EMP-0216", "Maya Kusuma", "maya.kusuma@simpul.co.id", "+62 811-2233-4455", "Design", "Product Designer", "active", 13_000_000, "female", date(2025, 3, 3)},
			{"EMP-0217", "Budi Raharjo", "budi.raharjo@simpul.co.id", "+62 812-4455-6677", "Engineering", "Engineering Manager", "active", 28_000_000, "male", date(2022, 9, 12)},
			{"EMP-0218", "Siti Rahma", "siti.rahma@simpul.co.id", "+62 815-6677-8899", "Marketing", "Social Media Specialist", "active", 8_500_000, "female", date(2024, 11, 20)},
			{"EMP-0219", "Fajar Nugroho", "fajar.nugroho@simpul.co.id", "+62 817-8899-0011", "Engineering", "Senior Backend Engineer", "active", 19_500_000, "male", date(2025, 6, 2)},
			{"EMP-0201", "Bagus Prasetyo", "bagus.prasetyo@simpul.co.id", "+62 818-1122-3344", "Marketing", "Social Media Specialist", "inactive", 8_000_000, "male", date(2023, 4, 17)},
		}
		for _, spec := range employeeSpecs {
			employee := domain.Employee{OrganizationID: organization.ID, EmployeeCode: spec.Code}
			if err := tx.Where("organization_id=? AND employee_code=?", organization.ID, spec.Code).Assign(domain.Employee{FullName: spec.Name, Email: spec.Email, Phone: spec.Phone, HireDate: spec.HireDate, Status: spec.Status, DepartmentID: departments[spec.Department].ID, PositionID: positions[spec.Position].ID, Address: "Jakarta, Indonesia", Education: "Sarjana / S1", Gender: spec.Gender}).FirstOrCreate(&employee).Error; err != nil {
				return err
			}
			comp := domain.Compensation{OrganizationID: organization.ID, EmployeeID: employee.ID}
			if err := tx.Where("employee_id=?", employee.ID).Assign(domain.Compensation{BaseSalary: spec.Salary, TaxStatus: "TK/0", BPJSHealthEnabled: true, BPJSEmploymentEnabled: true, EffectiveFrom: spec.HireDate}).FirstOrCreate(&comp).Error; err != nil {
				return err
			}
			employees[spec.Name] = seededEmployee{Employee: employee, Compensation: comp}
		}
		budi := employees["Budi Raharjo"].Employee
		sarah := employees["Sarah"].Employee
		dewi := employees["Dewi Lestari"].Employee
		rani := employees["Rani Wijaya"].Employee
		siti := employees["Siti Rahma"].Employee
		_ = tx.Model(&domain.Employee{}).Where("id IN ?", []string{rani.ID, employees["Aditya Pratama"].Employee.ID, employees["Fajar Nugroho"].Employee.ID}).Update("manager_id", budi.ID).Error
		_ = tx.Model(&domain.Employee{}).Where("id IN ?", []string{budi.ID, siti.ID, employees["Bagus Prasetyo"].Employee.ID}).Update("manager_id", dewi.ID).Error
		_ = tx.Model(&domain.Employee{}).Where("id=?", employees["Maya Kusuma"].Employee.ID).Update("manager_id", rani.ID).Error
		_ = tx.Model(&domain.Employee{}).Where("id=?", sarah.ID).Update("manager_id", dewi.ID).Error
		for name, department := range departments {
			head := sarah.ID
			if name == "Engineering" {
				head = budi.ID
			} else if name == "Design" {
				head = rani.ID
			} else if name == "Marketing" {
				head = siti.ID
			}
			_ = tx.Model(&department).Update("head_employee_id", head).Error
		}

		passwordHash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		userSpecs := []struct{ Name, Role string }{{"Sarah", "hr_admin"}, {"Rani Wijaya", "employee"}, {"Aditya Pratama", "employee"}, {"Budi Raharjo", "team_lead"}}
		users := map[string]domain.User{}
		for _, spec := range userSpecs {
			employee := employees[spec.Name].Employee
			user := domain.User{Email: employee.Email}
			if err := tx.Where("email=?", employee.Email).Assign(domain.User{PasswordHash: string(passwordHash)}).FirstOrCreate(&user).Error; err != nil {
				return err
			}
			membership := domain.Membership{OrganizationID: organization.ID, UserID: user.ID}
			if err := tx.Where("organization_id=? AND user_id=?", organization.ID, user.ID).Assign(domain.Membership{EmployeeID: &employee.ID, RoleID: roles[spec.Role].ID}).FirstOrCreate(&membership).Error; err != nil {
				return err
			}
			users[spec.Name] = user
		}

		leaveSpecs := []struct {
			Name, Color string
			Quota       int
		}{{"Cuti Tahunan", "amber", 12}, {"Cuti Sakit", "coral", 5}, {"Cuti Khusus", "sky", 3}}
		leaveTypes := map[string]domain.LeaveType{}
		for _, spec := range leaveSpecs {
			item := domain.LeaveType{OrganizationID: organization.ID, Name: spec.Name}
			if err := tx.Where("organization_id=? AND name=?", organization.ID, spec.Name).Assign(domain.LeaveType{AnnualQuota: spec.Quota, Color: spec.Color}).FirstOrCreate(&item).Error; err != nil {
				return err
			}
			leaveTypes[spec.Name] = item
			for _, employee := range employees {
				balance := domain.LeaveBalance{OrganizationID: organization.ID, EmployeeID: employee.Employee.ID, LeaveTypeID: item.ID, Year: now.Year()}
				if err := tx.Where("employee_id=? AND leave_type_id=? AND year=?", employee.Employee.ID, item.ID, now.Year()).Assign(domain.LeaveBalance{Quota: spec.Quota, Taken: 0}).FirstOrCreate(&balance).Error; err != nil {
					return err
				}
			}
		}
		leaveRequestSpecs := []struct {
			Employee, Type, Reason, Status string
			StartOffset, Days              int
		}{
			{"Rani Wijaya", "Cuti Tahunan", "Keperluan keluarga di luar kota", "pending", 10, 2},
			{"Aditya Pratama", "Cuti Sakit", "Pemulihan setelah demam", "pending", 3, 1},
			{"Fajar Nugroho", "Cuti Tahunan", "Menghadiri pernikahan saudara", "pending", 21, 3},
			{"Siti Rahma", "Cuti Tahunan", "Urusan administrasi pribadi", "pending", 14, 1},
			{"Maya Kusuma", "Cuti Khusus", "Menjadi pembicara di konferensi desain", "approved", -12, 2},
			{"Budi Raharjo", "Cuti Tahunan", "Liburan keluarga tahunan", "approved", -30, 5},
			{"Sarah", "Cuti Tahunan", "Acara keluarga besar", "rejected", -8, 1},
		}
		for _, spec := range leaveRequestSpecs {
			employee := employees[spec.Employee].Employee
			request := domain.LeaveRequest{
				OrganizationID: organization.ID, EmployeeID: employee.ID, LeaveTypeID: leaveTypes[spec.Type].ID,
				StartDate: dateFrom(now, spec.StartOffset), EndDate: dateFrom(now, spec.StartOffset+spec.Days-1),
			}
			if err := tx.Where("organization_id=? AND employee_id=? AND start_date=?", organization.ID, request.EmployeeID, request.StartDate).
				Assign(domain.LeaveRequest{DurationDays: spec.Days, Reason: spec.Reason, Status: spec.Status}).FirstOrCreate(&request).Error; err != nil {
				return err
			}
		}
		// Derive taken quota from approved requests so balances stay truthful and idempotent.
		if err := tx.Exec(`UPDATE leave_balances b SET taken = COALESCE((
			SELECT SUM(lr.duration_days) FROM leave_requests lr
			WHERE lr.employee_id = b.employee_id AND lr.leave_type_id = b.leave_type_id
			  AND lr.status = 'approved' AND EXTRACT(YEAR FROM lr.start_date) = b.year
		), 0) WHERE b.organization_id = ?`, organization.ID).Error; err != nil {
			return err
		}

		// Attendance history: the last three working weeks for every active employee so
		// dashboards, analytics and the attendance table all have real substance.
		activeEmployees := make([]domain.Employee, 0, len(employeeSpecs))
		for _, spec := range employeeSpecs {
			if spec.Status == "active" {
				activeEmployees = append(activeEmployees, employees[spec.Name].Employee)
			}
		}
		for empIndex, employee := range activeEmployees {
			for dayOffset := 20; dayOffset >= 0; dayOffset-- {
				day := dateFrom(now, -dayOffset)
				if weekday := day.Weekday(); weekday == time.Saturday || weekday == time.Sunday {
					continue
				}
				seedValue := empIndex*97 + dayOffset*13
				if noise(seedValue)%100 < 4 {
					continue // occasional absence keeps the anomaly column meaningful
				}
				clockInMinutes := 8*60 + 45 + noise(seedValue+1)%45
				status := "present"
				if clockInMinutes > 9*60+5 {
					status = "late"
				}
				overtime := 0
				if noise(seedValue+2)%100 < 35 {
					overtime = 30 + noise(seedValue+3)%90
				}
				clockIn := time.Date(day.Year(), day.Month(), day.Day(), clockInMinutes/60, clockInMinutes%60, 0, 0, day.Location())
				clockOut := time.Date(day.Year(), day.Month(), day.Day(), (17*60+15+overtime)/60, (17*60+15+overtime)%60, 0, 0, day.Location())
				totalMinutes := int(clockOut.Sub(clockIn).Minutes())
				attendance := domain.Attendance{OrganizationID: organization.ID, EmployeeID: employee.ID, Date: day}
				// Today's shift is still open when the seed runs during working hours.
				if dayOffset == 0 && now.Hour() < 17 {
					if err := tx.Where("employee_id=? AND date=?", employee.ID, attendance.Date).
						Assign(domain.Attendance{ClockIn: &clockIn, ClockOut: nil, TotalMinutes: 0, OvertimeMinutes: 0, Status: status, Source: "self"}).
						FirstOrCreate(&attendance).Error; err != nil {
						return err
					}
					continue
				}
				if err := tx.Where("employee_id=? AND date=?", employee.ID, attendance.Date).
					Assign(domain.Attendance{ClockIn: &clockIn, ClockOut: &clockOut, TotalMinutes: totalMinutes, OvertimeMinutes: overtime, Status: status, Source: "self"}).
					FirstOrCreate(&attendance).Error; err != nil {
					return err
				}
			}
		}
		// Weekly shift rota for the current week, varied per employee, with times that
		// match each shift type (the UI legend reads these back).
		shiftTimes := map[string][2]string{
			"morning":   {"08:00", "17:00"},
			"afternoon": {"12:00", "21:00"},
			"night":     {"20:00", "05:00"},
			"off":       {"", ""},
		}
		shiftPatterns := [][]string{
			{"morning", "morning", "morning", "morning", "morning", "off", "off"},
			{"morning", "morning", "afternoon", "afternoon", "morning", "off", "off"},
			{"afternoon", "afternoon", "morning", "morning", "afternoon", "off", "off"},
		}
		monday := dateFrom(now, -int((int(now.Weekday())+6)%7))
		for empIndex, spec := range employeeSpecs {
			pattern := shiftPatterns[empIndex%len(shiftPatterns)]
			employee := employees[spec.Name].Employee
			for dayIndex, shiftType := range pattern {
				times := shiftTimes[shiftType]
				shift := domain.ShiftAssignment{OrganizationID: organization.ID, EmployeeID: employee.ID, Date: monday.AddDate(0, 0, dayIndex)}
				if err := tx.Where("employee_id=? AND date=?", shift.EmployeeID, shift.Date).
					Assign(domain.ShiftAssignment{ShiftType: shiftType, StartTime: times[0], EndTime: times[1]}).
					FirstOrCreate(&shift).Error; err != nil {
					return err
				}
			}
		}

		cap1, cap2, cap3 := int64(60_000_000), int64(250_000_000), int64(500_000_000)
		policyConfig := payroll.Policy{BPJSHealthEmployeeRate: 0.01, BPJSEmploymentEmployeeRate: 0.02, BPJSMonthlyWageCap: 12_000_000, AnnualNonTaxableIncome: map[string]int64{"TK/0": 54_000_000, "K/0": 58_500_000, "K/1": 63_000_000}, ProgressiveTaxBrackets: []payroll.ProgressiveBracket{{UpTo: &cap1, Rate: 0.05}, {UpTo: &cap2, Rate: 0.15}, {UpTo: &cap3, Rate: 0.25}, {UpTo: nil, Rate: 0.30}}}
		policyRaw, _ := json.Marshal(policyConfig)
		policy := domain.PayrollPolicy{OrganizationID: organization.ID, Name: "Konfigurasi Demo Payroll Indonesia"}
		if err := tx.Where("organization_id=? AND name=?", organization.ID, policy.Name).Assign(domain.PayrollPolicy{EffectiveFrom: date(2026, 1, 1), Config: policyRaw, Certified: false}).FirstOrCreate(&policy).Error; err != nil {
			return err
		}
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		periodEnd := periodStart.AddDate(0, 1, -1)
		run := domain.PayrollRun{OrganizationID: organization.ID, PeriodStart: periodStart, PeriodEnd: periodEnd}
		if err := tx.Where("organization_id=? AND period_start=? AND period_end=?", organization.ID, periodStart, periodEnd).Assign(domain.PayrollRun{PolicyID: policy.ID, Status: "draft"}).FirstOrCreate(&run).Error; err != nil {
			return err
		}
		var totalGross, totalDeductions, totalNet int64
		for _, spec := range employeeSpecs {
			employee := employees[spec.Name]
			result := payroll.Calculate(payroll.Input{MonthlyGross: employee.Compensation.BaseSalary, TaxStatus: "TK/0", BPJSHealthEnabled: true, BPJSEmploymentEnabled: true}, policyConfig)
			breakdown, _ := json.Marshal(result)
			payslip := domain.Payslip{OrganizationID: organization.ID, PayrollRunID: run.ID, EmployeeID: employee.Employee.ID}
			if err := tx.Where("payroll_run_id=? AND employee_id=?", run.ID, employee.Employee.ID).Assign(domain.Payslip{GrossPay: result.GrossPay, BPJSDeduction: result.BPJSDeduction, PPh21Deduction: result.PPh21Deduction, NetPay: result.NetPay, Breakdown: breakdown}).FirstOrCreate(&payslip).Error; err != nil {
				return err
			}
			totalGross += result.GrossPay
			totalDeductions += result.BPJSDeduction + result.PPh21Deduction
			totalNet += result.NetPay
		}
		// Project the draft run's totals from its payslips so the dashboard reports a real
		// figure instead of Rp 0 before the run is executed.
		if err := tx.Model(&domain.PayrollRun{}).Where("id=?", run.ID).Updates(map[string]any{
			"total_gross": totalGross, "total_deductions": totalDeductions, "total_net": totalNet,
		}).Error; err != nil {
			return err
		}

		jobSpecs := []struct{ Title, Department string }{{"UI/UX Designer", "Design"}, {"DevOps Engineer", "Engineering"}, {"Product Manager", "Marketing"}, {"React Developer", "Engineering"}, {"QA Engineer", "Engineering"}, {"Security Engineer", "Engineering"}, {"Frontend Architect", "Engineering"}, {"Data Scientist", "Engineering"}}
		jobs := map[string]domain.JobPosting{}
		for _, spec := range jobSpecs {
			job := domain.JobPosting{OrganizationID: organization.ID, Title: spec.Title}
			if err := tx.Where("organization_id=? AND title=?", organization.ID, spec.Title).Assign(domain.JobPosting{DepartmentID: departments[spec.Department].ID, Status: "open", Description: "Lowongan demo Simpul"}).FirstOrCreate(&job).Error; err != nil {
				return err
			}
			jobs[spec.Title] = job
		}
		candidateSpecs := []struct {
			Name, Role, Source, Stage string
			Rating                    int
		}{{"Dewi Anggraini", "UI/UX Designer", "LinkedIn", "inbox", 4}, {"Fajar Nugraha", "DevOps Engineer", "Referral", "inbox", 5}, {"Grace Natalie", "Product Manager", "Indeed", "inbox", 3}, {"Hendra Wijaya", "React Developer", "LinkedIn", "screening", 4}, {"Indah Permata", "QA Engineer", "Kalibrr", "interview", 4}, {"Joko Santoso", "Security Engineer", "Referral", "interview", 5}, {"Kartika Sari", "Frontend Architect", "LinkedIn", "offer", 5}, {"Lukman Hakim", "Data Scientist", "LinkedIn", "hired", 4}, {"Nadia Utami", "UI/UX Designer", "Indeed", "rejected", 2}}
		candidates := map[string]domain.Candidate{}
		for index, spec := range candidateSpecs {
			candidate := domain.Candidate{OrganizationID: organization.ID, FullName: spec.Name}
			if err := tx.Where("organization_id=? AND full_name=?", organization.ID, spec.Name).Assign(domain.Candidate{JobPostingID: jobs[spec.Role].ID, Email: fmt.Sprintf("candidate%d@example.com", index+1), Source: spec.Source, Stage: spec.Stage, Rating: spec.Rating}).FirstOrCreate(&candidate).Error; err != nil {
				return err
			}
			candidates[spec.Name] = candidate
		}

		// The review cycle is anchored to the current month so the demo always has a
		// genuinely in-flight cycle instead of one whose end date has already passed.
		cycle := domain.PerformanceCycle{OrganizationID: organization.ID, Name: "Review Kinerja Semesteran"}
		cycleStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -2, 0)
		cycleEnd := cycleStart.AddDate(0, 6, -1)
		if err := tx.Where("organization_id=? AND name=?", organization.ID, cycle.Name).Assign(domain.PerformanceCycle{StartDate: cycleStart, EndDate: cycleEnd, Status: "active"}).FirstOrCreate(&cycle).Error; err != nil {
			return err
		}
		goalSpecs := []struct {
			Title, Category  string
			Progress, Weight int
		}{{"Optimalkan Latensi REST API Backend", "Engineering", 85, 40}, {"Redesain Halaman Dashboard Core HR", "Design", 100, 30}, {"Tingkatkan Dokumentasi API via OpenAPI", "Engineering", 50, 30}}
		aditya := employees["Aditya Pratama"].Employee
		for _, target := range []domain.Employee{aditya, sarah, employees["Rani Wijaya"].Employee, employees["Fajar Nugroho"].Employee} {
			for _, spec := range goalSpecs {
				goal := domain.Goal{OrganizationID: organization.ID, EmployeeID: target.ID, CycleID: cycle.ID, Title: spec.Title}
				if err := tx.Where("organization_id=? AND employee_id=? AND cycle_id=? AND title=?", organization.ID, target.ID, cycle.ID, spec.Title).Assign(domain.Goal{Category: spec.Category, Progress: spec.Progress, Weight: spec.Weight}).FirstOrCreate(&goal).Error; err != nil {
					return err
				}
			}
		}
		reviewSpecs := []struct {
			Employee, Reviewer, Relation, Notes string
			Rating                              int
		}{
			{"Aditya Pratama", "Sarah", "Manager", "Pekerjaan yang luar biasa di kuartal ini.", 5},
			{"Sarah", "Aditya Pratama", "Peer", "Kolaborasi People Ops sangat responsif dan terukur.", 5},
			{"Rani Wijaya", "Budi Raharjo", "Manager", "Desain sistem konsisten dan mudah diimplementasikan tim.", 4},
			{"Budi Raharjo", "Sarah", "Peer", "Kepemimpinan tim Engineering stabil sepanjang siklus.", 4},
			{"Fajar Nugroho", "Budi Raharjo", "Manager", "Onboarding cepat dan kontribusi teknis langsung terasa.", 4},
		}
		for _, spec := range reviewSpecs {
			review := domain.Review{OrganizationID: organization.ID, CycleID: cycle.ID, EmployeeID: employees[spec.Employee].Employee.ID, ReviewerID: employees[spec.Reviewer].Employee.ID}
			if err := tx.Where("organization_id=? AND cycle_id=? AND employee_id=? AND reviewer_id=?", organization.ID, cycle.ID, review.EmployeeID, review.ReviewerID).Assign(domain.Review{Relation: spec.Relation, Rating: spec.Rating, Notes: spec.Notes}).FirstOrCreate(&review).Error; err != nil {
				return err
			}
		}

		for _, provider := range []string{"slack", "google", "entra"} {
			integration := domain.Integration{OrganizationID: organization.ID, Provider: provider}
			if err := tx.Where("organization_id=? AND provider=?", organization.ID, provider).Assign(domain.Integration{Enabled: false}).FirstOrCreate(&integration).Error; err != nil {
				return err
			}
		}
		faqSpecs := []struct{ Category, Question, Answer string }{{"Absensi", "Bagaimana cara merevisi absensi yang lupa clock-in?", "Buka menu Hadir lalu gunakan Entri Manual."}, {"Payroll", "Kapan PPh 21 dihitung?", "Potongan dihitung ketika payroll run dieksekusi menggunakan policy efektif."}, {"Cuti", "Bagaimana menambah kuota cuti?", "HR Admin dapat mengubah kebijakan dan saldo cuti."}, {"Sistem", "Bagaimana mengaktifkan SSO?", "Buka Pengaturan, pilih Integrasi, lalu lengkapi konfigurasi provider."}}
		for index, spec := range faqSpecs {
			faq := domain.FAQ{OrganizationID: organization.ID, Question: spec.Question}
			if err := tx.Where("organization_id=? AND question=?", organization.ID, spec.Question).Assign(domain.FAQ{Category: spec.Category, Answer: spec.Answer, SortOrder: index + 1}).FirstOrCreate(&faq).Error; err != nil {
				return err
			}
		}
		// Notifications for every demo persona so the bell is never empty.
		notificationSpecs := []struct{ User, Title, Message, Kind string }{
			{"Sarah", "Kontrak Berakhir", "2 kontrak karyawan berakhir dalam 30 hari ke depan.", "urgent"},
			{"Sarah", "Payroll Draf", "Payroll periode berjalan menunggu untuk dijalankan.", "info"},
			{"Sarah", "Absensi Terlambat", "3 catatan keterlambatan menunggu verifikasi hari ini.", "info"},
			{"Budi Raharjo", "Pengajuan Cuti Baru", "Aditya Pratama mengajukan cuti sakit dan menunggu persetujuan Anda.", "urgent"},
			{"Budi Raharjo", "Sasaran Tim Diperbarui", "Rani Wijaya memperbarui progres sasaran kuartal ini.", "info"},
			{"Budi Raharjo", "Tinjauan Kinerja", "Siklus review pertengahan tahun menunggu umpan balik Anda.", "info"},
			{"Aditya Pratama", "Slip Gaji Tersedia", "Slip gaji periode berjalan sudah dapat diunduh.", "info"},
			{"Aditya Pratama", "Pengingat Absen Keluar", "Anda belum melakukan absen keluar hari ini.", "urgent"},
			{"Rani Wijaya", "Cuti Disetujui", "Pengajuan cuti khusus Anda telah disetujui.", "info"},
			{"Rani Wijaya", "Tinjauan Rekan Kerja", "Satu permintaan umpan balik 360° menunggu Anda.", "info"},
		}
		for _, spec := range notificationSpecs {
			notification := domain.Notification{OrganizationID: organization.ID, UserID: users[spec.User].ID, Title: spec.Title}
			if err := tx.Where("organization_id=? AND user_id=? AND title=?", organization.ID, notification.UserID, spec.Title).
				Assign(domain.Notification{Message: spec.Message, Kind: spec.Kind, Payload: json.RawMessage("{}")}).
				FirstOrCreate(&notification).Error; err != nil {
				return err
			}
		}

		// A couple of support tickets so the help centre has history.
		supportSpecs := []struct {
			User, Subject, Category, Message, Status string
		}{
			{"Aditya Pratama", "Lupa absen masuk kemarin", "Absensi", "Saya lupa clock-in saat bekerja dari kantor cabang. Mohon dibantu koreksi.", "open"},
			{"Rani Wijaya", "Pertanyaan potongan BPJS", "Payroll", "Mohon penjelasan rincian potongan BPJS pada slip gaji bulan ini.", "resolved"},
			{"Budi Raharjo", "Akses laporan analitik", "Sistem / Teknis", "Menu analitik tidak menampilkan data tim saya. Mohon diperiksa.", "open"},
		}
		for _, spec := range supportSpecs {
			ticket := domain.SupportTicket{OrganizationID: organization.ID, RequesterID: users[spec.User].ID, Subject: spec.Subject}
			if err := tx.Where("organization_id=? AND requester_id=? AND subject=?", organization.ID, ticket.RequesterID, spec.Subject).
				Assign(domain.SupportTicket{Category: spec.Category, Message: spec.Message, Status: spec.Status}).
				FirstOrCreate(&ticket).Error; err != nil {
				return err
			}
		}
		conversationSpecs := []struct {
			Contact  string
			Messages []struct{ From, Body string }
		}{
			{"Aditya Pratama", []struct{ From, Body string }{
				{"Aditya Pratama", "Halo Sarah, apakah slip gaji periode ini sudah bisa diunduh?"},
				{"Sarah", "Halo Adit, sudah tersedia di menu Payroll. Silakan dicek ya."},
				{"Aditya Pratama", "Baik, terima kasih. Sudah saya unduh."},
			}},
			{"Rani Wijaya", []struct{ From, Body string }{
				{"Rani Wijaya", "Sarah, saya sudah mengajukan cuti khusus untuk konferensi desain."},
				{"Sarah", "Sudah masuk ke antrean persetujuan Budi. Saya pantau ya."},
				{"Rani Wijaya", "Terima kasih banyak!"},
			}},
			{"Budi Raharjo", []struct{ From, Body string }{
				{"Budi Raharjo", "Sarah, ada 3 pengajuan cuti tim saya yang menunggu."},
				{"Sarah", "Betul, dua di antaranya masih menunggu keputusan Anda."},
				{"Budi Raharjo", "Oke, saya review hari ini."},
			}},
		}
		for _, spec := range conversationSpecs {
			conversation := domain.Conversation{OrganizationID: organization.ID, Title: spec.Contact}
			if err := tx.Where("organization_id=? AND title=?", organization.ID, spec.Contact).FirstOrCreate(&conversation).Error; err != nil {
				return err
			}
			for _, userID := range []string{users["Sarah"].ID, users[spec.Contact].ID} {
				member := domain.ConversationMember{OrganizationID: organization.ID, ConversationID: conversation.ID, UserID: userID}
				if err := tx.Where("conversation_id=? AND user_id=?", conversation.ID, userID).FirstOrCreate(&member).Error; err != nil {
					return err
				}
			}
			for _, message := range spec.Messages {
				entry := domain.Message{OrganizationID: organization.ID, ConversationID: conversation.ID, SenderUserID: users[message.From].ID, Body: message.Body}
				if err := tx.Where("conversation_id=? AND sender_user_id=? AND body=?", conversation.ID, entry.SenderUserID, entry.Body).FirstOrCreate(&entry).Error; err != nil {
					return err
				}
			}
		}

		// A realistic audit trail. Every mutating handler writes these rows during normal
		// use, so without seeding the audit view would be empty on a fresh install.
		// Request IDs are deterministic, which keeps the seeder idempotent on re-run.
		// Only entity types that handlers genuinely audit are used here.
		auditSpecs := []struct {
			Actor, Action, EntityType, EntityID string
			DaysAgo, Hour, Minute               int
		}{
			{"Sarah", "update", "organization", organization.ID, 21, 9, 12},
			{"Sarah", "create", "department", departments["Marketing"].ID, 20, 10, 5},
			{"Sarah", "create", "employee", employees["Bagus Prasetyo"].Employee.ID, 19, 14, 30},
			{"Sarah", "create", "employee", employees["Siti Rahma"].Employee.ID, 19, 14, 38},
			{"Sarah", "update", "employee", employees["Rani Wijaya"].Employee.ID, 14, 11, 45},
			{"Budi Raharjo", "update", "employee", employees["Aditya Pratama"].Employee.ID, 13, 9, 20},
			{"Sarah", "permissions_changed", "role", roles["team_lead"].ID, 12, 16, 10},
			{"Sarah", "create", "candidate", candidates["Hendra Wijaya"].ID, 11, 10, 30},
			{"Sarah", "stage_change", "candidate", candidates["Kartika Sari"].ID, 9, 15, 25},
			{"Sarah", "execute", "payroll_run", run.ID, 6, 8, 5},
			{"Sarah", "password_changed", "user", users["Sarah"].ID, 5, 17, 40},
			{"Budi Raharjo", "update", "employee", employees["Fajar Nugroho"].Employee.ID, 3, 13, 15},
			{"Sarah", "update", "employee", employees["Maya Kusuma"].Employee.ID, 2, 9, 55},
			{"Sarah", "create", "employee", employees["Siti Rahma"].Employee.ID, 1, 11, 20},
		}
		for index, spec := range auditSpecs {
			requestID := fmt.Sprintf("seed-audit-%02d", index+1)
			// audit_logs is append-only at the database level (a trigger rejects UPDATE
			// and DELETE), so this must never attempt an upsert. Skip rows that exist.
			var existing int64
			if err := tx.Model(&domain.AuditLog{}).
				Where("organization_id=? AND request_id=?", organization.ID, requestID).
				Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				continue
			}
			actorID := users[spec.Actor].ID
			createdAt := dateFrom(now, -spec.DaysAgo).
				Add(time.Duration(spec.Hour)*time.Hour + time.Duration(spec.Minute)*time.Minute)
			entry := domain.AuditLog{
				OrganizationID: organization.ID,
				ActorUserID:    &actorID,
				RequestID:      requestID,
				Action:         spec.Action,
				EntityType:     spec.EntityType,
				EntityID:       spec.EntityID,
				CreatedAt:      createdAt,
			}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// noise derives a stable pseudo-random integer from a seed so re-running the seeder
// produces the same demo dataset instead of shuffling values on every run.
func noise(seed int) int {
	value := seed*2654435761 + 1013904223
	value ^= value >> 15
	value *= 2246822519
	value ^= value >> 13
	if value < 0 {
		value = -value
	}
	return value
}

func keys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
func dateFrom(value time.Time, days int) time.Time {
	result := value.AddDate(0, 0, days)
	return time.Date(result.Year(), result.Month(), result.Day(), 0, 0, 0, 0, result.Location())
}
func mustLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}
