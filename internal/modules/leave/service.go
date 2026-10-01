package leave

import "time"

func WorkingDays(start, end time.Time, holidays map[string]bool) int {
	if end.Before(start) {
		return 0
	}
	count := 0
	for day := dateOnly(start); !day.After(dateOnly(end)); day = day.AddDate(0, 0, 1) {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			continue
		}
		if holidays[day.Format("2006-01-02")] {
			continue
		}
		count++
	}
	return count
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func Overlaps(start, end, existingStart, existingEnd time.Time) bool {
	return !start.After(existingEnd) && !end.Before(existingStart)
}
