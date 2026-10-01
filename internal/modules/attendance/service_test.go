package attendance

import (
	"testing"
	"time"
)

func TestDurationSeparatesOvertime(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Jakarta")
	clockIn := time.Date(2026, 8, 21, 8, 0, 0, 0, location)
	clockOut := time.Date(2026, 8, 21, 18, 30, 0, 0, location)
	minutes, overtime := Duration(clockIn, clockOut)
	status := StatusFor(clockIn, 9, 0)
	if minutes != 630 || overtime != 150 || status != "present" {
		t.Fatalf("unexpected calculation: minutes=%d overtime=%d status=%s", minutes, overtime, status)
	}
}

func TestDurationRejectsClockOutBeforeClockIn(t *testing.T) {
	now := time.Now()
	minutes, overtime := Duration(now, now.Add(-time.Minute))
	if minutes != 0 || overtime != 0 {
		t.Fatal("invalid duration must be zero")
	}
}
