package httpapi

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type problemDocument struct {
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Status    int               `json:"status"`
	Code      string            `json:"code"`
	Detail    string            `json:"detail"`
	Errors    map[string]string `json:"errors,omitempty"`
	RequestID string            `json:"requestId,omitempty"`
}

type listMeta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
	Total    int64 `json:"total"`
}

func tenantReferenceExists(db *gorm.DB, model any, organizationID, entityID string) bool {
	if entityID == "" {
		return false
	}
	var count int64
	return db.Model(model).Where("organization_id=? AND id=?", organizationID, entityID).Count(&count).Error == nil && count == 1
}

func problem(c *gin.Context, status int, code, title, detail string, fields map[string]string) {
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, problemDocument{
		Type: "https://simpul.local/problems/" + code, Title: title, Status: status,
		Code: code, Detail: detail, Errors: fields, RequestID: c.GetString("requestId"),
	})
}

func databaseProblem(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		problem(c, http.StatusNotFound, "not_found", "Data tidak ditemukan", "Resource yang diminta tidak tersedia.", nil)
		return
	}
	problem(c, http.StatusInternalServerError, "database_error", "Terjadi kesalahan", "Operasi database gagal diproses.", nil)
}

func pagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", "25"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return
}

func parseDate(value string) (time.Time, error) { return time.Parse("2006-01-02", value) }

// formatCSVDate renders a date column the way the UI does, without a time component.
func formatCSVDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

// formatCSVTime renders an optional timestamp as a local wall-clock time.
func formatCSVTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04")
}

// writeCSV streams a UTF-8 CSV attachment. `encoding/csv` handles RFC 4180 quoting, and
// a leading byte-order mark keeps Excel from mangling accented names — a real problem
// for Indonesian HR exports, even though the BOM is invisible in other tools.
func writeCSV(c *gin.Context, filename string, header []string, rows [][]string) {
	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(&buffer)
	if err := writer.Write(header); err != nil {
		exportProblem(c)
		return
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			exportProblem(c)
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		exportProblem(c)
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buffer.Bytes())
}

func exportProblem(c *gin.Context) {
	problem(c, http.StatusInternalServerError, "export_failed", "Ekspor gagal", "Berkas CSV tidak dapat disusun.", nil)
}

func validUUID(value string) bool { _, err := uuid.Parse(value); return err == nil }

func requestID() string { return uuid.NewString() }

func requestMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if id == "" {
			id = requestID()
		}
		c.Set("requestId", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}
