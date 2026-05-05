package serveranalytics

import (
	"testing"
	"time"
)

func TestCompletedDayRange(t *testing.T) {
	now := time.Date(2026, time.May, 5, 12, 34, 0, 0, time.UTC)
	start, stop := CompletedDayRange(now, 14)

	wantStart := time.Date(2026, time.April, 21, 0, 0, 0, 0, time.UTC)
	wantStop := time.Date(2026, time.May, 4, 23, 59, 59, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Fatalf("start = %s, want %s", start, wantStart)
	}
	if !stop.Equal(wantStop) {
		t.Fatalf("stop = %s, want %s", stop, wantStop)
	}
}

func TestCompletedDayRangeCacheWindow(t *testing.T) {
	now := time.Date(2026, time.May, 5, 0, 31, 0, 0, time.UTC)
	start, stop := CompletedDayRange(now, CacheDays)

	wantStart := time.Date(2026, time.February, 4, 0, 0, 0, 0, time.UTC)
	wantStop := time.Date(2026, time.May, 4, 23, 59, 59, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Fatalf("start = %s, want %s", start, wantStart)
	}
	if !stop.Equal(wantStop) {
		t.Fatalf("stop = %s, want %s", stop, wantStop)
	}
}
