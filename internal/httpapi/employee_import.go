package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/simpul/hr-backend/internal/domain"
	"github.com/simpul/hr-backend/internal/modules/employeeimport"
	"github.com/simpul/hr-backend/internal/repository"
	"gorm.io/gorm"
)

// maxImportBytes bounds an uploaded roster. A few thousand rows is well under this, and
// the limit stops a large or careless upload from being buffered in full before any
// validation could reject it.
const maxImportBytes = 4 << 20

// importReport is the response for both a dry run and a real import, so the interface can
// render the same panel either way.
type importReport struct {
	DryRun  bool                        `json:"dryRun"`
	Valid   int                         `json:"valid"`
	Created int                         `json:"created"`
	Errors  []employeeimport.FieldError `json:"errors"`
}

type positionRef struct {
	id           string
	departmentID string
}

// preparedRow is a validated row with its reference names resolved to ids.
type preparedRow struct {
	fullName     string
	employeeCode string
	email        string
	phone        string
	departmentID string
	positionID   string
	managerID    *string
	status       string
	baseSalary   int64
	hireDate     string
}

// importEmployees applies a CSV roster.
//
// The contract is all-or-nothing: every row is validated first, and if a single row is
// wrong nothing is written and the complete list of problems is returned. A half-applied
// roster is far harder to recover from than a rejected one, and it keeps the counts in
// the report honest — `created` is never a number the caller has to reconstruct.
func (s *Server) importEmployees(c *gin.Context) {
	id := identityFrom(c)
	dryRun := c.Query("dryRun") == "true"

	// The body is read in full before parsing so an oversized upload can be reported as
	// exactly that. Passing the reader straight to the parser would surface the size limit
	// as "this is not a CSV file", which sends the reader looking in the wrong place.
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			problem(c, http.StatusRequestEntityTooLarge, "file_too_large", "Berkas terlalu besar",
				fmt.Sprintf("Batas unggahan %d MB. Bagi berkas menjadi beberapa bagian.", maxImportBytes>>20), nil)
			return
		}
		problem(c, http.StatusBadRequest, "invalid_file", "Berkas tidak dapat dibaca", "Unggahan tidak dapat dibaca.", nil)
		return
	}

	parsed, err := employeeimport.Parse(bytes.NewReader(raw))
	if err != nil {
		problem(c, http.StatusBadRequest, "invalid_file", "Berkas tidak dapat diproses", err.Error(), nil)
		return
	}

	departments, positions, managers, codes, emails, err := s.importReferenceIndexes(id.OrganizationID)
	if err != nil {
		databaseProblem(c, err)
		return
	}

	// Per-row problems accumulate here. Parsing problems come first so the report reads in
	// file order once resolution problems are appended.
	fieldErrors := make([]employeeimport.FieldError, 0, len(parsed.Errors))
	fieldErrors = append(fieldErrors, parsed.Errors...)

	ready := make([]preparedRow, 0, len(parsed.Rows))
	for _, row := range parsed.Rows {
		departmentID, found := departments[strings.ToLower(row.DepartmentName)]
		if !found {
			fieldErrors = append(fieldErrors, employeeimport.FieldError{
				Line: row.Line, Column: employeeimport.ColumnDepartment,
				Message: fmt.Sprintf("Departemen %q belum terdaftar di workspace ini.", row.DepartmentName),
			})
			continue
		}

		position, found := positions[strings.ToLower(row.PositionTitle)]
		if !found {
			fieldErrors = append(fieldErrors, employeeimport.FieldError{
				Line: row.Line, Column: employeeimport.ColumnPosition,
				Message: fmt.Sprintf("Jabatan %q belum terdaftar di workspace ini.", row.PositionTitle),
			})
			continue
		}
		// Mirrors the rule enforced when creating a single employee: a position belongs to
		// exactly one department, and pairing it with another would produce an inconsistent
		// record that the rest of the application assumes cannot exist.
		if position.departmentID != departmentID {
			fieldErrors = append(fieldErrors, employeeimport.FieldError{
				Line: row.Line, Column: employeeimport.ColumnPosition,
				Message: fmt.Sprintf("Jabatan %q tidak terdaftar pada departemen %q.", row.PositionTitle, row.DepartmentName),
			})
			continue
		}

		// Existing rows are checked here rather than left to the database constraint, so the
		// message names the offending line instead of surfacing an opaque unique-violation.
		if _, taken := codes[strings.ToLower(row.EmployeeCode)]; taken {
			fieldErrors = append(fieldErrors, employeeimport.FieldError{
				Line: row.Line, Column: employeeimport.ColumnCode,
				Message: fmt.Sprintf("Kode karyawan %q sudah dipakai karyawan lain.", row.EmployeeCode),
			})
			continue
		}
		if _, taken := emails[strings.ToLower(row.Email)]; taken {
			fieldErrors = append(fieldErrors, employeeimport.FieldError{
				Line: row.Line, Column: employeeimport.ColumnEmail,
				Message: fmt.Sprintf("Email %q sudah dipakai karyawan lain.", row.Email),
			})
			continue
		}

		var managerID *string
		if row.ManagerName != "" {
			resolved, found := managers[strings.ToLower(row.ManagerName)]
			if !found {
				fieldErrors = append(fieldErrors, employeeimport.FieldError{
					Line: row.Line, Column: employeeimport.ColumnManager,
					Message: fmt.Sprintf("Manajer %q tidak ditemukan.", row.ManagerName),
				})
				continue
			}
			managerID = &resolved
		}

		ready = append(ready, preparedRow{
			fullName:     row.FullName,
			employeeCode: row.EmployeeCode,
			email:        strings.ToLower(row.Email),
			phone:        row.Phone,
			departmentID: departmentID,
			positionID:   position.id,
			managerID:    managerID,
			status:       row.Status,
			baseSalary:   row.BaseSalary,
			hireDate:     row.HireDate.Format("2006-01-02"),
		})
	}

	report := importReport{DryRun: dryRun, Valid: len(ready), Errors: fieldErrors}
	if report.Errors == nil {
		report.Errors = []employeeimport.FieldError{}
	}
	// Parsing problems and reference-resolution problems are collected in separate passes,
	// so the combined list is not in file order. Sorting by line makes the report read like
	// the spreadsheet, which is what the person fixing it is looking at.
	sort.SliceStable(report.Errors, func(first, second int) bool {
		if report.Errors[first].Line != report.Errors[second].Line {
			return report.Errors[first].Line < report.Errors[second].Line
		}
		return report.Errors[first].Column < report.Errors[second].Column
	})

	if len(fieldErrors) > 0 {
		// 422 rather than 400: the request was well formed, the data was not. Nothing has
		// been written at this point.
		c.JSON(http.StatusUnprocessableEntity, report)
		return
	}

	if dryRun {
		c.JSON(http.StatusOK, report)
		return
	}

	err = s.store.Transaction(c, id.OrganizationID, func(tx *gorm.DB) error {
		for _, row := range ready {
			hireDate, err := parseDate(row.hireDate)
			if err != nil {
				return err
			}
			employee := domain.Employee{
				OrganizationID: id.OrganizationID,
				EmployeeCode:   row.employeeCode,
				FullName:       row.fullName,
				Email:          row.email,
				Phone:          row.phone,
				HireDate:       hireDate,
				Status:         row.status,
				DepartmentID:   row.departmentID,
				PositionID:     row.positionID,
				ManagerID:      row.managerID,
			}
			if err := tx.Create(&employee).Error; err != nil {
				return err
			}
			// A compensation row is created alongside every employee, as the single-employee
			// endpoint does. Payroll reads it unconditionally, so skipping it here would
			// produce imported staff who silently cannot be paid.
			compensation := domain.Compensation{
				OrganizationID: id.OrganizationID, EmployeeID: employee.ID,
				BaseSalary: row.baseSalary, TaxStatus: "TK/0",
				BPJSHealthEnabled: true, BPJSEmploymentEnabled: true,
				EffectiveFrom: hireDate,
			}
			if err := tx.Create(&compensation).Error; err != nil {
				return err
			}
			if err := repository.Audit(tx, id.OrganizationID, id.UserID, c.GetString("requestId"), "create", "employee", employee.ID, nil, employee); err != nil {
				return err
			}
			report.Created++
		}
		return nil
	})
	if err != nil {
		report.Created = 0
		databaseProblem(c, err)
		return
	}

	c.JSON(http.StatusCreated, report)
}

// importReferenceIndexes loads the lookup tables the import resolves against, in five
// queries rather than three per row: a 500-row file would otherwise issue 1500 round
// trips before writing anything.
func (s *Server) importReferenceIndexes(organizationID string) (
	departments map[string]string,
	positions map[string]positionRef,
	managers map[string]string,
	codes map[string]struct{},
	emails map[string]struct{},
	err error,
) {
	var departmentRows []domain.Department
	if err = s.db.Model(&domain.Department{}).Where("organization_id=?", organizationID).Find(&departmentRows).Error; err != nil {
		return
	}
	departments = make(map[string]string, len(departmentRows))
	for _, item := range departmentRows {
		departments[strings.ToLower(strings.TrimSpace(item.Name))] = item.ID
	}

	var positionRows []domain.Position
	if err = s.db.Model(&domain.Position{}).Where("organization_id=?", organizationID).Find(&positionRows).Error; err != nil {
		return
	}
	positions = make(map[string]positionRef, len(positionRows))
	for _, item := range positionRows {
		positions[strings.ToLower(strings.TrimSpace(item.Title))] = positionRef{id: item.ID, departmentID: item.DepartmentID}
	}

	var employeeRows []struct {
		ID           string
		FullName     string
		EmployeeCode string
		Email        string
	}
	if err = s.db.Model(&domain.Employee{}).
		Select("id", "full_name", "employee_code", "email").
		Where("organization_id=? AND deleted_at IS NULL", organizationID).
		Find(&employeeRows).Error; err != nil {
		return
	}
	managers = make(map[string]string, len(employeeRows))
	codes = make(map[string]struct{}, len(employeeRows))
	emails = make(map[string]struct{}, len(employeeRows))
	for _, item := range employeeRows {
		managers[strings.ToLower(strings.TrimSpace(item.FullName))] = item.ID
		codes[strings.ToLower(strings.TrimSpace(item.EmployeeCode))] = struct{}{}
		emails[strings.ToLower(strings.TrimSpace(item.Email))] = struct{}{}
	}

	return departments, positions, managers, codes, emails, nil
}
