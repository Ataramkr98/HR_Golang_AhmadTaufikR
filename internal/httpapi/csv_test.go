package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// csvResponse menjalankan writeCSV pada konteks gin tiruan dan mengembalikan
// recorder-nya, sehingga isi berkas dan header dapat diperiksa tanpa server sungguhan.
func csvResponse(t *testing.T, header []string, rows [][]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	writeCSV(context, "contoh.csv", header, rows)
	return recorder
}

func TestWriteCSVProducesExcelFriendlyFile(t *testing.T) {
	recorder := csvResponse(t,
		[]string{"Nama", "Catatan"},
		[][]string{{"Aditya Pratama", "tepat waktu"}},
	)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, mau 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="contoh.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	// Tanpa no-store, berkas berisi data gaji dapat tertinggal di cache peramban bersama.
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, mau no-store", got)
	}

	body := recorder.Body.String()
	// BOM UTF-8: tanpa ini Excel di Windows merusak nama beraksen, masalah nyata untuk
	// ekspor kepegawaian Indonesia meski BOM-nya tidak terlihat di alat lain.
	if !strings.HasPrefix(body, "\ufeff") {
		t.Error("berkas CSV tidak diawali BOM UTF-8")
	}
	if !strings.Contains(body, "Nama,Catatan") {
		t.Errorf("baris header tidak ditemukan pada %q", body)
	}
}

func TestWriteCSVQuotesAmbiguousValues(t *testing.T) {
	// Nilai yang mengandung koma, tanda kutip, dan baris baru adalah kasus yang paling
	// mudah merusak CSV. encoding/csv menanganinya; test ini menjaga agar kita tidak
	// pernah menggantinya dengan penggabungan string manual.
	recorder := csvResponse(t,
		[]string{"Catatan"},
		[][]string{{"terlambat, \"macet\"\nlalu lembur"}},
	)

	body := strings.TrimPrefix(recorder.Body.String(), "\ufeff")
	want := "\"terlambat, \"\"macet\"\"\nlalu lembur\"\n"
	if !strings.HasSuffix(body, want) {
		t.Errorf("nilai tidak dikutip sesuai RFC 4180.\n got: %q\nwant suffix: %q", body, want)
	}
}

func TestWriteCSVWritesOnlyTheHeaderWhenThereAreNoRows(t *testing.T) {
	// Ekspor yang tidak menemukan baris tetap harus berupa berkas CSV yang sah, bukan
	// respons kosong, supaya peramban tidak menyimpan berkas nol byte.
	recorder := csvResponse(t, []string{"Nama"}, nil)

	body := strings.TrimPrefix(recorder.Body.String(), "\ufeff")
	if body != "Nama\n" {
		t.Errorf("body = %q, mau %q", body, "Nama\n")
	}
}

func TestExportProblemIsAProblemDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	exportProblem(context)

	if recorder.Code != 500 {
		t.Fatalf("status = %d, mau 500", recorder.Code)
	}
	var document problemDocument
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("body bukan JSON: %v", err)
	}
	if document.Code != "export_failed" {
		t.Errorf("code = %q, mau export_failed", document.Code)
	}
	if document.Status != 500 {
		t.Errorf("status dokumen = %d, mau 500", document.Status)
	}
}

func TestFormatCSVDates(t *testing.T) {
	// Tanggal ditulis sebagai YYYY-MM-DD agar Excel di lokal mana pun membacanya sebagai
	// tanggal, bukan menebak format regional dan membalik hari dengan bulan.
	if got := formatCSVDate(time.Date(2026, time.September, 15, 13, 45, 0, 0, time.UTC)); got != "2026-09-15" {
		t.Errorf("formatCSVDate = %q, mau 2026-09-15", got)
	}
	if got := formatCSVDate(time.Time{}); got != "" {
		t.Errorf("formatCSVDate(zero) = %q, mau kosong", got)
	}

	clockIn := time.Date(2026, time.September, 15, 9, 26, 0, 0, time.UTC)
	if got := formatCSVTime(&clockIn); got != "2026-09-15 09:26" {
		t.Errorf("formatCSVTime = %q, mau 2026-09-15 09:26", got)
	}
	// Karyawan yang belum absen keluar harus menghasilkan sel kosong, bukan "0001-01-01".
	if got := formatCSVTime(nil); got != "" {
		t.Errorf("formatCSVTime(nil) = %q, mau kosong", got)
	}
}
