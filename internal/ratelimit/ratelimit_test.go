package ratelimit

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func clock(l *Limiter, start time.Time) *time.Time {
	now := start
	l.now = func() time.Time { return now }
	return &now
}

func TestRefusesAfterMaxFailures(t *testing.T) {
	l := New()
	clock(l, t0)
	for i := range MaxFailures - 1 {
		l.Failure("ip")
		if d := l.RetryAfter("ip"); d != 0 {
			t.Fatalf("refused after %d failures", i+1)
		}
	}
	l.Failure("ip")
	if d := l.RetryAfter("ip"); d != window {
		t.Errorf("RetryAfter = %v, want %v", d, window)
	}
}

func TestWaitCountsFromOldestFailure(t *testing.T) {
	l := New()
	now := clock(l, t0)
	for range MaxFailures {
		l.Failure("ip")
		*now = now.Add(time.Minute)
	}
	if d := l.RetryAfter("ip"); d != 10*time.Minute {
		t.Errorf("RetryAfter = %v, want 10m", d)
	}
}

func TestFailuresAgeOut(t *testing.T) {
	l := New()
	now := clock(l, t0)
	for range MaxFailures {
		l.Failure("ip")
	}
	*now = t0.Add(window + time.Second)
	if d := l.RetryAfter("ip"); d != 0 {
		t.Errorf("RetryAfter = %v after window, want 0", d)
	}
}

func TestSuccessClearsFailures(t *testing.T) {
	l := New()
	clock(l, t0)
	for range MaxFailures {
		l.Failure("ip")
	}
	l.Success("ip")
	if d := l.RetryAfter("ip"); d != 0 {
		t.Errorf("RetryAfter = %v after success, want 0", d)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New()
	clock(l, t0)
	for range MaxFailures {
		l.Failure("a")
	}
	if d := l.RetryAfter("b"); d != 0 {
		t.Errorf("key b refused: %v", d)
	}
}

func TestFailureSweepsStaleKeysOncePerInterval(t *testing.T) {
	l := New()
	now := clock(l, t0)
	l.Failure("stale")
	*now = t0.Add(window + time.Minute)
	for range MaxFailures {
		l.Failure("blocked")
	}
	if _, ok := l.failures["stale"]; ok {
		t.Error("stale key kept after sweep")
	}
	if d := l.RetryAfter("blocked"); d <= 0 {
		t.Error("sweep unblocked a blocked key")
	}
	l.Failure("stale")
	*now = now.Add(window + time.Minute)
	l.Failure("other")
	*now = now.Add(time.Second)
	l.Failure("other")
	if len(l.failures) != 1 {
		t.Errorf("keys = %d after sweep, want only other", len(l.failures))
	}
}
