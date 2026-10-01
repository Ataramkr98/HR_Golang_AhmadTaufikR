package leave

import (
	"testing"
	"time"
)

func TestWorkingDaysExcludesWeekendAndHoliday(t *testing.T) {
	start, _ := time.Parse("2006-01-02", "2026-07-13")
	end, _ := time.Parse("2006-01-02", "2026-07-19")
	got := WorkingDays(start, end, map[string]bool{"2026-07-15": true})
	if got != 4 {
		t.Fatalf("working days = %d, want 4", got)
	}
}
