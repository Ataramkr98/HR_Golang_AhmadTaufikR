// Package employeeimport parses and validates a CSV roster before anything is written to
// the database.
//
// It is deliberately free of database and HTTP concerns: it answers "is this file
// well-formed, and which rows are wrong?" so the caller can decide whether to apply it.
// That split keeps the fiddly parsing rules unit-testable without a Postgres instance.
package employeeimport

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Column labels match the CSV export byte-for-byte, so a file produced by "Ekspor CSV"
// can be edited and fed straight back in without renaming anything.
const (
	ColumnFullName   = "Nama"
	ColumnCode       = "Kode Karyawan"
	ColumnEmail      = "Email"
	ColumnPhone      = "Telepon"
	ColumnDepartment = "Departemen"
	ColumnPosition   = "Jabatan"
	ColumnManager    = "Manajer"
	ColumnStatus     = "Status"
	ColumnHireDate   = "Tanggal Masuk"
	ColumnBaseSalary = "Gaji Pokok"
)

// RequiredColumns must all be present. Optional columns may be omitted entirely, which
// keeps a hand-written three-column file usable.
var RequiredColumns = []string{
	ColumnFullName, ColumnCode, ColumnEmail, ColumnDepartment, ColumnPosition, ColumnHireDate,
}

// OptionalColumns are read when present and ignored when absent. Gaji Pokok is optional
// because the export deliberately omits salary, yet bulk onboarding without it would
// leave every imported employee unpayable.
var OptionalColumns = []string{ColumnPhone, ColumnManager, ColumnStatus, ColumnBaseSalary}

// statusAliases accepts both the API's enum values and the Indonesian labels the
// interface displays. A person editing the export in a spreadsheet will plausibly type
// "Aktif" rather than "active", and rejecting that would be needlessly pedantic.
var statusAliases = map[string]string{
	"active":     "active",
	"aktif":      "active",
	"on_leave":   "on_leave",
	"cuti":       "on_leave",
	"inactive":   "inactive",
	"non-aktif":  "inactive",
	"nonaktif":   "inactive",
	"terminated": "terminated",
	"berhenti":   "terminated",
}

// DefaultStatus is applied when the status column is absent or blank.
const DefaultStatus = "active"

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s.]+\.[^@\s]+$`)

// ThousandsPattern accepts whole rupiah written with "." or "," as the thousands
// separator, which is how the figure appears in Indonesian spreadsheets.
var thousandsPattern = regexp.MustCompile(`^\d{1,3}([.,]\d{3})*$`)

// plainDigitsPattern is the unseparated form, e.g. 18000000.
var plainDigitsPattern = regexp.MustCompile(`^\d+$`)

// Row is one validated data row, with reference names still unresolved: turning
// "Engineering" into a department id needs the database, which this package does not see.
type Row struct {
	Line           int
	FullName       string
	EmployeeCode   string
	Email          string
	Phone          string
	DepartmentName string
	PositionTitle  string
	ManagerName    string
	Status         string
	HireDate       time.Time
	BaseSalary     int64
}

// FieldError reports one problem on one line. It names the line as it appears in the
// spreadsheet so the person fixing the file can find it.
type FieldError struct {
	Line    int    `json:"line"`
	Column  string `json:"column"`
	Message string `json:"message"`
}

// Result is the outcome of parsing: the rows that passed, and every problem found.
type Result struct {
	Rows   []Row
	Errors []FieldError
}

// FileError describes a problem with the file as a whole — it is not CSV at all, or a
// required column is missing. Per-row problems are returned as FieldError instead, so
// callers can tell "this file is unusable" apart from "these five rows need fixing".
type FileError struct{ Message string }

func (e *FileError) Error() string { return e.Message }

// IsFileError reports whether err came from a file-level problem.
func IsFileError(err error) bool {
	var target *FileError
	return errors.As(err, &target)
}

// Parse reads a CSV roster and validates every row.
//
// It never stops at the first bad row: the whole point is to hand back one complete list
// of problems so the file can be fixed in a single pass rather than one upload per typo.
func Parse(source io.Reader) (Result, error) {
	reader := csv.NewReader(source)
	// Ragged rows are a common spreadsheet export problem and deserve a per-line message
	// rather than an opaque reader error, so the field count is not enforced here.
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return Result{}, &FileError{"Berkas kosong. Ekspor CSV terlebih dahulu untuk mendapatkan templat."}
	}
	if err != nil {
		return Result{}, &FileError{"Berkas tidak dapat dibaca sebagai CSV. Pastikan berkas berformat CSV, bukan Excel (.xlsx)."}
	}

	columns := map[string]int{}
	for position, name := range header {
		columns[normalizeHeader(name)] = position
	}

	var missing []string
	for _, required := range RequiredColumns {
		if _, found := columns[required]; !found {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return Result{}, &FileError{fmt.Sprintf(
			"Kolom wajib tidak ditemukan: %s. Gunakan tombol Ekspor CSV sebagai templat.",
			strings.Join(missing, ", "),
		)}
	}

	result := Result{Rows: make([]Row, 0, 16)}
	seenCodes := map[string]int{}
	seenEmails := map[string]int{}
	// line always holds the most recently known spreadsheet line. FieldPos reports the
	// real line, including when a quoted cell spans several lines; it is only zero before
	// the first successful read, which is why the counter is kept as a fallback.
	line := 0

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A quoting error mid-file is reported against its line and parsing continues
			// where the reader can resume, so one broken cell does not hide the rest.
			if position, _ := reader.FieldPos(0); position > 0 {
				line = position
			} else {
				line++
			}
			result.Errors = append(result.Errors, FieldError{Line: line, Message: "Baris tidak dapat dibaca: " + err.Error()})
			continue
		}
		if position, _ := reader.FieldPos(0); position > 0 {
			line = position
		} else {
			line++
		}
		if isBlankRecord(record) {
			continue
		}

		row, fieldErrors := validateRecord(record, columns, line, seenCodes, seenEmails)
		result.Errors = append(result.Errors, fieldErrors...)
		if len(fieldErrors) == 0 {
			result.Rows = append(result.Rows, row)
		}
	}

	return result, nil
}

func validateRecord(record []string, columns map[string]int, line int, seenCodes, seenEmails map[string]int) (Row, []FieldError) {
	row := Row{
		Line:           line,
		FullName:       value(record, columns, ColumnFullName),
		EmployeeCode:   value(record, columns, ColumnCode),
		Email:          value(record, columns, ColumnEmail),
		Phone:          value(record, columns, ColumnPhone),
		DepartmentName: value(record, columns, ColumnDepartment),
		PositionTitle:  value(record, columns, ColumnPosition),
		ManagerName:    value(record, columns, ColumnManager),
	}
	var problems []FieldError
	add := func(column, message string) {
		problems = append(problems, FieldError{Line: line, Column: column, Message: message})
	}

	if row.FullName == "" {
		add(ColumnFullName, "Nama wajib diisi.")
	}
	if row.EmployeeCode == "" {
		add(ColumnCode, "Kode karyawan wajib diisi.")
	}
	if row.Email == "" {
		add(ColumnEmail, "Email wajib diisi.")
	} else if !emailPattern.MatchString(row.Email) {
		add(ColumnEmail, fmt.Sprintf("Format email tidak valid: %q.", row.Email))
	}
	if row.DepartmentName == "" {
		add(ColumnDepartment, "Departemen wajib diisi.")
	}
	if row.PositionTitle == "" {
		add(ColumnPosition, "Jabatan wajib diisi.")
	}

	hireDate := value(record, columns, ColumnHireDate)
	switch {
	case hireDate == "":
		add(ColumnHireDate, "Tanggal masuk wajib diisi.")
	default:
		parsed, err := time.Parse("2006-01-02", hireDate)
		if err != nil {
			add(ColumnHireDate, fmt.Sprintf("Tanggal masuk harus berformat YYYY-MM-DD, diterima %q.", hireDate))
		} else {
			row.HireDate = parsed
		}
	}

	status := strings.ToLower(value(record, columns, ColumnStatus))
	if status == "" {
		row.Status = DefaultStatus
	} else if resolved, ok := statusAliases[status]; ok {
		row.Status = resolved
	} else {
		add(ColumnStatus, fmt.Sprintf("Status %q tidak dikenal. Gunakan active, on_leave, inactive, atau terminated.", status))
	}

	// Salary is optional and written as whole rupiah. A decimal point is rejected rather
	// than guessed at: "18000000.00" could plausibly mean 18 million or 1.8 billion once
	// separators are stripped, and silently picking one would corrupt payroll.
	salary := value(record, columns, ColumnBaseSalary)
	if salary != "" {
		switch {
		case plainDigitsPattern.MatchString(salary):
			parsed, err := strconv.ParseInt(salary, 10, 64)
			if err != nil {
				add(ColumnBaseSalary, fmt.Sprintf("Gaji pokok %q di luar jangkauan.", salary))
			} else {
				row.BaseSalary = parsed
			}
		case thousandsPattern.MatchString(salary):
			parsed, err := strconv.ParseInt(strings.NewReplacer(".", "", ",", "").Replace(salary), 10, 64)
			if err != nil {
				add(ColumnBaseSalary, fmt.Sprintf("Gaji pokok %q di luar jangkauan.", salary))
			} else {
				row.BaseSalary = parsed
			}
		default:
			add(ColumnBaseSalary, fmt.Sprintf("Gaji pokok %q tidak valid. Tulis angka rupiah bulat, misalnya 18000000.", salary))
		}
	}

	// Duplicates inside one file are almost always a copy-paste slip, and they would
	// otherwise surface as a confusing database constraint error later.
	if row.EmployeeCode != "" {
		if first, duplicate := seenCodes[row.EmployeeCode]; duplicate {
			add(ColumnCode, fmt.Sprintf("Kode karyawan %q sudah dipakai pada baris %d.", row.EmployeeCode, first))
		} else {
			seenCodes[row.EmployeeCode] = line
		}
	}
	if row.Email != "" {
		key := strings.ToLower(row.Email)
		if first, duplicate := seenEmails[key]; duplicate {
			add(ColumnEmail, fmt.Sprintf("Email %q sudah dipakai pada baris %d.", row.Email, first))
		} else {
			seenEmails[key] = line
		}
	}

	return row, problems
}

func value(record []string, columns map[string]int, column string) string {
	position, found := columns[column]
	if !found || position >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[position])
}

// normalizeHeader drops the UTF-8 byte-order mark and surrounding whitespace. The export
// writes a BOM so Excel renders Indonesian names correctly, which means the very first
// header cell arrives as "\ufeffNama" unless it is stripped here.
func normalizeHeader(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))
}

func isBlankRecord(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
