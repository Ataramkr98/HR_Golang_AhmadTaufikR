package attendance

import "time"

func Duration(clockIn, clockOut time.Time) (totalMinutes, overtimeMinutes int) {
	if !clockOut.After(clockIn) {
		return 0, 0
	}
	totalMinutes = int(clockOut.Sub(clockIn).Minutes())
	if totalMinutes > 8*60 {
		overtimeMinutes = totalMinutes - 8*60
	}
	return totalMinutes, overtimeMinutes
}

func StatusFor(clockIn time.Time, scheduledHour, scheduledMinute int) string {
	scheduled := time.Date(clockIn.Year(), clockIn.Month(), clockIn.Day(), scheduledHour, scheduledMinute, 0, 0, clockIn.Location())
	if clockIn.After(scheduled.Add(5 * time.Minute)) {
		return "late"
	}
	return "present"
}
