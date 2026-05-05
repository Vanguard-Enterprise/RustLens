package serveranalytics

import "time"

const CacheDays = 90

// CompletedDayRange returns an inclusive UTC range covering the last n completed days.
func CompletedDayRange(now time.Time, days int) (time.Time, time.Time) {
	if days < 1 {
		days = 1
	}
	today := now.UTC()
	yesterday := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	startDay := yesterday.AddDate(0, 0, -(days - 1))
	start := time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, time.UTC)
	stop := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 0, time.UTC)
	return start, stop
}
