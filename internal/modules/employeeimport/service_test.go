package employeeimport

import (
	"strings"
	"testing"
)

const header = "Nama,Kode Karyawan,Email,Telepon,Departemen,Jabatan,Manajer,Status,Tanggal Masuk"

func parse(t *testing.T, body string) Result {
	t.Helper()
	result, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse returned a file error: %v", err)
	}
	return result
}

func TestParseAcceptsAnExportedFile(t *testing.T) {
	// The body is exactly what the export writes, BOM included, so this test fails if the
	// export and the import ever disagree about the header.
	body := "\ufeff" + header + "\n" +
		"Aditya Pratama,EMP-0215,aditya.p@simpul.co.id,+62 813-9876-5432,Engineering,Senior Backend Engineer,Budi Raharjo,active,2024-02-10\n"

	result := parse(t, body)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
	row := result.Rows[0]
	if row.FullName != "Aditya Pratama" || row.EmployeeCode != "EMP-0215" {
		t.Errorf("identity mis-parsed: %+v", row)
	}
	if row.DepartmentName != "Engineering" || row.PositionTitle != "Senior Backend Engineer" {
		t.Errorf("placement mis-parsed: %+v", row)
	}
	if row.ManagerName != "Budi Raharjo" || row.Status != "active" {
		t.Errorf("manager/status mis-parsed: %+v", row)
	}
	if got := row.HireDate.Format("2006-01-02"); got != "2024-02-10" {
		t.Errorf("hire date = %s, want 2024-02-10", got)
	}
	// The header is line 1, so the first data row is line 2.
	if row.Line != 2 {
		t.Errorf("line = %d, want 2", row.Line)
	}
}

func TestParseIgnoresColumnOrder(t *testing.T) {
	// People rearrange columns in a spreadsheet; the import should follow the header, not
	// assume a fixed order.
	body := "Tanggal Masuk,Jabatan,Nama,Departemen,Email,Kode Karyawan\n" +
		"2025-01-06,Product Designer,Rani Wijaya,Design,rani.wijaya@simpul.co.id,EMP-0214\n"

	result := parse(t, body)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	row := result.Rows[0]
	if row.FullName != "Rani Wijaya" || row.PositionTitle != "Product Designer" || row.DepartmentName != "Design" {
		t.Errorf("column mapping wrong: %+v", row)
	}
	if row.Status != DefaultStatus {
		t.Errorf("status = %q, want the %q default when the column is absent", row.Status, DefaultStatus)
	}
}

func TestParseReportsAMissingRequiredColumnOnce(t *testing.T) {
	_, err := Parse(strings.NewReader("Nama,Email\nAditya,aditya@simpul.co.id\n"))

	if err == nil {
		t.Fatal("expected a file error for a missing required column")
	}
	if !IsFileError(err) {
		t.Fatalf("error type = %T, want a FileError", err)
	}
	// The message must name the missing columns so the file can actually be fixed.
	for _, column := range []string{ColumnCode, ColumnDepartment, ColumnPosition, ColumnHireDate} {
		if !strings.Contains(err.Error(), column) {
			t.Errorf("error %q does not mention the missing column %q", err.Error(), column)
		}
	}
}

func TestParseRejectsAnEmptyFile(t *testing.T) {
	_, err := Parse(strings.NewReader(""))
	if err == nil || !IsFileError(err) {
		t.Fatalf("expected a FileError for an empty file, got %v", err)
	}
}

func TestParseReportsEveryBadRowRatherThanStoppingAtTheFirst(t *testing.T) {
	// One complete pass should surface every problem, so the file can be fixed once
	// instead of re-uploaded once per typo.
	body := header + "\n" +
		"Aditya Pratama,EMP-0215,aditya.p@simpul.co.id,,Engineering,Senior Backend Engineer,,active,2024-02-10\n" +
		"Rani Wijaya,EMP-0214,not-an-email,,Design,Product Designer,,active,2025-01-06\n" +
		"Fajar Nugroho,EMP-0219,fajar@simpul.co.id,,Engineering,Backend Engineer,,active,31/12/2024\n"

	result := parse(t, body)

	if len(result.Rows) != 1 {
		t.Fatalf("valid rows = %d, want 1", len(result.Rows))
	}
	if len(result.Errors) != 2 {
		t.Fatalf("errors = %d (%+v), want 2", len(result.Errors), result.Errors)
	}
	// Line numbers must point at the spreadsheet rows, and name the offending column.
	if result.Errors[0].Line != 3 || result.Errors[0].Column != ColumnEmail {
		t.Errorf("first error = %+v, want line 3 on Email", result.Errors[0])
	}
	if result.Errors[1].Line != 4 || result.Errors[1].Column != ColumnHireDate {
		t.Errorf("second error = %+v, want line 4 on Tanggal Masuk", result.Errors[1])
	}
	// A day-first date must be rejected, not silently reinterpreted as something else.
	if !strings.Contains(result.Errors[1].Message, "YYYY-MM-DD") {
		t.Errorf("date error does not explain the expected format: %q", result.Errors[1].Message)
	}
}

func TestParseAcceptsStatusLabelsShownInTheInterface(t *testing.T) {
	// The table renders "Aktif"/"Cuti"/"Berhenti", so those are what a person editing the
	// file by hand is most likely to type.
	body := header + "\n" +
		"A,EMP-1,a@simpul.co.id,,Engineering,Engineer,,Aktif,2024-01-01\n" +
		"B,EMP-2,b@simpul.co.id,,Engineering,Engineer,,Cuti,2024-01-01\n" +
		"C,EMP-3,c@simpul.co.id,,Engineering,Engineer,,Berhenti,2024-01-01\n"

	result := parse(t, body)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	for index, want := range []string{"active", "on_leave", "terminated"} {
		if result.Rows[index].Status != want {
			t.Errorf("row %d status = %q, want %q", index, result.Rows[index].Status, want)
		}
	}
}

func TestParseRejectsAnUnknownStatus(t *testing.T) {
	body := header + "\nA,EMP-1,a@simpul.co.id,,Engineering,Engineer,,pensiun,2024-01-01\n"

	result := parse(t, body)

	if len(result.Errors) != 1 || result.Errors[0].Column != ColumnStatus {
		t.Fatalf("errors = %+v, want one on Status", result.Errors)
	}
	if len(result.Rows) != 0 {
		t.Error("a row with an invalid status must not be imported")
	}
}

func TestParseFlagsDuplicatesInsideTheSameFile(t *testing.T) {
	// Duplicates are a copy-paste slip that would otherwise surface much later as an
	// opaque database constraint error.
	body := header + "\n" +
		"Aditya Pratama,EMP-0215,aditya@simpul.co.id,,Engineering,Engineer,,active,2024-01-01\n" +
		"Aditya Duplikat,EMP-0215,aditya2@simpul.co.id,,Engineering,Engineer,,active,2024-01-01\n" +
		"Rani Wijaya,EMP-0214,ADITYA@simpul.co.id,,Design,Designer,,active,2024-01-01\n"

	result := parse(t, body)

	if len(result.Errors) != 2 {
		t.Fatalf("errors = %+v, want 2", result.Errors)
	}
	if result.Errors[0].Column != ColumnCode || !strings.Contains(result.Errors[0].Message, "baris 2") {
		t.Errorf("code duplicate not reported against its first line: %+v", result.Errors[0])
	}
	// Email comparison must be case-insensitive, since addresses are.
	if result.Errors[1].Column != ColumnEmail || !strings.Contains(result.Errors[1].Message, "baris 2") {
		t.Errorf("email duplicate not reported case-insensitively: %+v", result.Errors[1])
	}
	if len(result.Rows) != 1 {
		t.Errorf("rows = %d, want only the first to survive", len(result.Rows))
	}
}

func TestParseHandlesQuotedValuesAndBlankLines(t *testing.T) {
	body := header + "\n" +
		"\"Wijaya, Rani\",EMP-0214,rani@simpul.co.id,,Design,\"Product Designer, Senior\",,active,2025-01-06\n" +
		",,,,,,,,\n" +
		"\n"

	result := parse(t, body)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1 (blank lines skipped)", len(result.Rows))
	}
	if result.Rows[0].FullName != "Wijaya, Rani" {
		t.Errorf("quoted comma not preserved: %q", result.Rows[0].FullName)
	}
	if result.Rows[0].PositionTitle != "Product Designer, Senior" {
		t.Errorf("quoted position not preserved: %q", result.Rows[0].PositionTitle)
	}
}

func TestParseReportsTheRealLineWhenAQuotedCellSpansLines(t *testing.T) {
	// A multi-line cell shifts every subsequent row, so counting records instead of
	// consulting the reader's position would point at the wrong line.
	body := header + "\n" +
		"\"Baris satu\nbaris dua\",EMP-1,a@simpul.co.id,,Engineering,Engineer,,active,2024-01-01\n" +
		"Rani Wijaya,EMP-2,bad-email,,Design,Designer,,active,2024-01-01\n"

	result := parse(t, body)

	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
	if len(result.Errors) != 1 {
		t.Fatalf("errors = %+v, want 1", result.Errors)
	}
	// The multi-line cell occupies lines 2-3, so the bad row starts on line 4.
	if result.Errors[0].Line != 4 {
		t.Errorf("line = %d, want 4", result.Errors[0].Line)
	}
}

func TestParseReadsOptionalBaseSalary(t *testing.T) {
	body := header + ",Gaji Pokok\n" +
		"A,EMP-1,a@simpul.co.id,,Engineering,Engineer,,active,2024-01-01,18000000\n" +
		"B,EMP-2,b@simpul.co.id,,Engineering,Engineer,,active,2024-01-01,18.000.000\n" +
		"C,EMP-3,c@simpul.co.id,,Engineering,Engineer,,active,2024-01-01,\n"

	result := parse(t, body)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	for index, want := range []int64{18000000, 18000000, 0} {
		if result.Rows[index].BaseSalary != want {
			t.Errorf("row %d salary = %d, want %d", index, result.Rows[index].BaseSalary, want)
		}
	}
}

func TestParseRejectsAnAmbiguousSalary(t *testing.T) {
	// Stripping separators from "18000000.00" would silently multiply the figure by 100,
	// so the ambiguous form is refused instead of guessed at.
	body := header + ",Gaji Pokok\nA,EMP-1,a@simpul.co.id,,Engineering,Engineer,,active,2024-01-01,18000000.00\n"

	result := parse(t, body)

	if len(result.Errors) != 1 || result.Errors[0].Column != ColumnBaseSalary {
		t.Fatalf("errors = %+v, want one on Gaji Pokok", result.Errors)
	}
	if len(result.Rows) != 0 {
		t.Error("a row with an unparseable salary must not be imported")
	}
}

func TestParseAcceptsAMinimalHandWrittenFile(t *testing.T) {
	// Optional columns may be omitted entirely; the minimum viable file should work.
	body := "Nama,Kode Karyawan,Email,Departemen,Jabatan,Tanggal Masuk\n" +
		"Dewi Lestari,EMP-0000,dewi@simpul.co.id,People (HR),Chief Executive Officer,2023-05-02\n"

	result := parse(t, body)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
	if result.Rows[0].Status != DefaultStatus {
		t.Errorf("status = %q, want %q", result.Rows[0].Status, DefaultStatus)
	}
}
