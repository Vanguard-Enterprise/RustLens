package httpapi

import (
	"testing"
	"time"

	"rustlens/backend/internal/store"
)

func TestUtcWeekdayMondayZero(t *testing.T) {
	// 2026-01-05 00:00 UTC is a Monday — the reference base for the analytics
	// computation. Verify the helper returns 0 for that day and increments
	// through the week.
	cases := []struct {
		dayOffsetDays int
		want          int
		label         string
	}{
		{0, 0, "Monday"},
		{1, 1, "Tuesday"},
		{2, 2, "Wednesday"},
		{3, 3, "Thursday"},
		{4, 4, "Friday"},
		{5, 5, "Saturday"},
		{6, 6, "Sunday"},
		{7, 0, "Next Monday"},
	}

	for _, tc := range cases {
		got := utcWeekdayMondayZero(trackAnalyticsRefDayUnix + int64(tc.dayOffsetDays)*86400)
		if got != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.label, got, tc.want)
		}
	}
}

func TestDistributeSessionAcrossDays(t *testing.T) {
	// A 90-minute session that starts at 23:30 on Monday and ends at 01:00
	// on Tuesday must be split into three buckets:
	//   - Mon hour 23: 30 min
	//   - Tue hour 0:  60 min
	monStart := time.Date(2026, 1, 5, 23, 30, 0, 0, time.UTC).Unix()
	tueStop := time.Date(2026, 1, 6, 1, 0, 0, 0, time.UTC).Unix()

	type segment struct {
		dayUnix int64
		hour    int
		seconds int64
	}
	got := []segment{}
	distributeSessionAcrossDays(monStart, tueStop, func(dayUnix int64, hour int, seconds int64) {
		got = append(got, segment{dayUnix, hour, seconds})
	})

	monDay := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC).Unix()
	tueDay := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC).Unix()
	want := []segment{
		{monDay, 23, 30 * 60},
		{tueDay, 0, 60 * 60},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segment %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestComputeTrackAnalyticsDays_DropsShortDays(t *testing.T) {
	// Player A: 1 hour on Monday hour 14, on a single date — passes the 60 min
	// threshold and should contribute 60 minutes to (Monday, 14).
	monHour14Start := time.Date(2026, 1, 5, 14, 0, 0, 0, time.UTC).Unix()
	monHour14Stop := monHour14Start + 3600

	// Player B: 10 min only on Monday — under the 60 min threshold, must be
	// dropped entirely.
	monShortStart := time.Date(2026, 1, 12, 18, 0, 0, 0, time.UTC).Unix()
	monShortStop := monShortStart + 10*60

	sessions := map[string][]store.TrackAnalyticsSession{
		"playerA": {{StartUnix: monHour14Start, StopUnix: monHour14Stop}},
		"playerB": {{StartUnix: monShortStart, StopUnix: monShortStop}},
	}

	days := computeTrackAnalyticsDays(sessions)
	if len(days) != 7 {
		t.Fatalf("expected 7 weekdays, got %d", len(days))
	}

	monday := days[0]
	if monday.Weekday != "Monday" {
		t.Fatalf("expected Monday, got %s", monday.Weekday)
	}

	// Find the (Monday, hour 14) entry — its avg should be 60 from player A
	// (player B was dropped for being under threshold).
	hour14Unix := trackAnalyticsRefDayUnix + 14*3600
	var hour14 *trackAnalyticsHourValue
	for i := range monday.Hours {
		if monday.Hours[i].HourUnix == hour14Unix {
			hour14 = &monday.Hours[i]
			break
		}
	}
	if hour14 == nil {
		t.Fatalf("Monday hour 14 missing from results")
	}
	if hour14.Avg != 60.0 || hour14.Minutes != 60.0 {
		t.Fatalf("Monday hour 14 minutes: got avg=%f minutes=%f, want 60", hour14.Avg, hour14.Minutes)
	}

	// Hour 18 should have 0 (only player B was online there, but they were
	// dropped for being under-threshold).
	hour18Unix := trackAnalyticsRefDayUnix + 18*3600
	for _, hv := range monday.Hours {
		if hv.HourUnix == hour18Unix && hv.Avg != 0 {
			t.Fatalf("Monday hour 18 avg should be 0 (player B dropped), got %f", hv.Avg)
		}
	}
}

func TestComputeTrackAnalyticsDays_UsesLondonTimezone(t *testing.T) {
	// 23:00 UTC in June is 00:00 Tuesday in Europe/London, so this session must
	// be counted on Tuesday hour 0, not Monday hour 23.
	start := time.Date(2026, 6, 1, 23, 0, 0, 0, time.UTC).Unix()
	stop := start + 60*60
	sessions := map[string][]store.TrackAnalyticsSession{
		"playerA": {{StartUnix: start, StopUnix: stop}},
	}

	days := computeTrackAnalyticsDays(sessions)
	tuesday := days[1]
	if tuesday.Hours[0].Minutes != 60 {
		t.Fatalf("Tuesday London hour 0 minutes: got %f, want 60", tuesday.Hours[0].Minutes)
	}
	if days[0].Hours[23].Minutes != 0 {
		t.Fatalf("Monday London hour 23 minutes: got %f, want 0", days[0].Hours[23].Minutes)
	}
}

func TestBestOfflineWindow_LowestPair(t *testing.T) {
	hours := [trackAnalyticsHoursPerDay]float64{}
	// Make hour 3 + hour 4 the lowest pair (both 0.1).
	for h := 0; h < trackAnalyticsHoursPerDay; h++ {
		hours[h] = 5.0
	}
	hours[3] = 0.1
	hours[4] = 0.1

	start, end := bestOfflineWindow(hours, trackAnalyticsRefDayUnix)
	if start != trackAnalyticsRefDayUnix+3*3600 {
		t.Fatalf("best window start: got %d, want %d", start, trackAnalyticsRefDayUnix+3*3600)
	}
	if end-start != 2*3600 {
		t.Fatalf("best window length: got %d, want %d seconds", end-start, 2*3600)
	}
}
