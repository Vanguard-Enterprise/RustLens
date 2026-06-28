package wipereactscheduler

import (
	"testing"
	"time"

	"rustlens/backend/internal/store"
)

func mustUnix(t *testing.T, tz string, value string) int64 {
	t.Helper()
	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatalf("load location %q: %v", tz, err)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed.Unix()
}

// A fresh "Wednesday 19:00, post 1 day before" schedule created Wednesday 20:00
// must NOT enqueue a reminder for today's already-passed 19:00 wipe. It should
// pick next week's wipe instead.
func TestComputeNextPostTime_FreshScheduleSkipsPastWipe(t *testing.T) {
	const tz = "Europe/London"
	now := mustUnix(t, tz, "2026-06-24 20:00") // Wednesday 20:00, after the 19:00 wipe

	schedule := store.WipeReactSchedule{
		Days:       []string{"wednesday"},
		Time:       "19:00",
		TimeZone:   tz,
		Repeat:     "weekly",
		PostBefore: "1d",
	}

	next := computeNextPostTime(schedule, now)
	if next == nil {
		t.Fatal("expected a next post time, got nil")
	}

	if next.wipeUnix <= now {
		t.Fatalf("wipe time must be in the future: wipeUnix=%d now=%d", next.wipeUnix, now)
	}

	wantWipe := mustUnix(t, tz, "2026-07-01 19:00") // following Wednesday
	if next.wipeUnix != wantWipe {
		t.Fatalf("expected next wipe %d, got %d", wantWipe, next.wipeUnix)
	}
}

// A legitimate "post N days before a future wipe" lead time must still produce a
// postUnix in the past/present (so the reminder fires now) for a future wipe.
func TestComputeNextPostTime_LeadTimeForFutureWipe(t *testing.T) {
	const tz = "Europe/London"
	// Tuesday 19:30, one day-and-a-bit before Wednesday 19:00.
	now := mustUnix(t, tz, "2026-06-23 19:30")

	schedule := store.WipeReactSchedule{
		Days:       []string{"wednesday"},
		Time:       "19:00",
		TimeZone:   tz,
		Repeat:     "weekly",
		PostBefore: "1d",
	}

	next := computeNextPostTime(schedule, now)
	if next == nil {
		t.Fatal("expected a next post time, got nil")
	}

	wantWipe := mustUnix(t, tz, "2026-06-24 19:00")
	if next.wipeUnix != wantWipe {
		t.Fatalf("expected next wipe %d, got %d", wantWipe, next.wipeUnix)
	}
	if next.wipeUnix <= now {
		t.Fatalf("wipe must be in the future: wipeUnix=%d now=%d", next.wipeUnix, now)
	}
	// postUnix is wipe - 1 day = Tuesday 19:00, which is <= now, so the reminder
	// is due immediately for a future wipe — the legitimate lead-time case.
	if next.postUnix > now {
		t.Fatalf("expected postUnix <= now (due now): postUnix=%d now=%d", next.postUnix, now)
	}
}
