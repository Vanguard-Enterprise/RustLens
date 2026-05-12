package cleanup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	wipeReactCalls   int
	analyticsCalls   int
	wipeReactCutoffs []time.Time
	analyticsCutoffs []time.Time
	wipeReactRows    int64
	analyticsRows    int64
	wipeReactErr     error
	analyticsErr     error
}

func (f *fakeStore) PurgeWipeReactsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	f.wipeReactCalls++
	f.wipeReactCutoffs = append(f.wipeReactCutoffs, cutoff)
	return f.wipeReactRows, f.wipeReactErr
}

func (f *fakeStore) PurgeTrackAnalyticsSessionsCacheBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	f.analyticsCalls++
	f.analyticsCutoffs = append(f.analyticsCutoffs, cutoff)
	return f.analyticsRows, f.analyticsErr
}

func TestRunOnceCallsAllPurgesWithRetentionCutoffs(t *testing.T) {
	store := &fakeStore{}
	cfg := Config{
		Interval:                time.Hour,
		WipeReactRetention:      7 * 24 * time.Hour,
		AnalyticsCacheRetention: 48 * time.Hour,
	}
	svc := NewService(store, cfg)

	before := time.Now()
	svc.runOnce(context.Background())
	after := time.Now()

	if store.wipeReactCalls != 1 || store.analyticsCalls != 1 {
		t.Fatalf("expected 1 call per purge, got %+v", store)
	}

	checkCutoff := func(name string, cutoff time.Time, retention time.Duration) {
		t.Helper()
		expectedMin := before.Add(-retention).Add(-time.Second)
		expectedMax := after.Add(-retention).Add(time.Second)
		if cutoff.Before(expectedMin) || cutoff.After(expectedMax) {
			t.Errorf("%s cutoff outside expected window: got=%s expected~[%s, %s]", name, cutoff, expectedMin, expectedMax)
		}
	}
	checkCutoff("wipeReact", store.wipeReactCutoffs[0], cfg.WipeReactRetention)
	checkCutoff("analytics", store.analyticsCutoffs[0], cfg.AnalyticsCacheRetention)
}

func TestConfigDefaultsAreConservative(t *testing.T) {
	svc := NewService(&fakeStore{}, Config{})
	if svc.cfg.Interval != time.Hour {
		t.Errorf("Interval default = %s, want 1h", svc.cfg.Interval)
	}
	if svc.cfg.WipeReactRetention != 7*24*time.Hour {
		t.Errorf("WipeReactRetention default = %s, want 7d", svc.cfg.WipeReactRetention)
	}
	if svc.cfg.AnalyticsCacheRetention != 48*time.Hour {
		t.Errorf("AnalyticsCacheRetention default = %s, want 48h", svc.cfg.AnalyticsCacheRetention)
	}
}

func TestRunOnceIsRobustToOneStepFailing(t *testing.T) {
	store := &fakeStore{
		wipeReactErr: errors.New("boom"),
	}
	svc := NewService(store, Config{})
	// Should not panic and should still run the other step.
	svc.runOnce(context.Background())
	if store.analyticsCalls != 1 {
		t.Fatalf("expected analytics to still run after wipeReact failure, got %+v", store)
	}
}

func TestRunReturnsWhenContextCancelled(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, Config{Interval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after context cancel")
	}
}
