package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func clock(l *Limiter, start time.Time) *time.Time {
	now := start
	l.now = func() time.Time { return now }
	return &now
}

func TestWaitCountsFromOldestFailure(t *testing.T) {
	l := New()
	now := clock(l, t0)
	for range MaxFailures {
		l.Attempt("ip")
		*now = now.Add(time.Minute)
	}
	if d := l.Attempt("ip"); d != 10*time.Minute {
		t.Errorf("Attempt = %v, want 10m", d)
	}
}

func TestFailuresAgeOut(t *testing.T) {
	l := New()
	now := clock(l, t0)
	for range MaxFailures {
		l.Attempt("ip")
	}
	*now = t0.Add(window + time.Second)
	if d := l.Attempt("ip"); d != 0 {
		t.Errorf("Attempt = %v after window, want 0", d)
	}
}

func TestSuccessClearsFailures(t *testing.T) {
	l := New()
	clock(l, t0)
	for range MaxFailures {
		l.Attempt("ip")
	}
	l.Success("ip")
	if d := l.Attempt("ip"); d != 0 {
		t.Errorf("Attempt = %v after success, want 0", d)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New()
	clock(l, t0)
	for range MaxFailures {
		l.Attempt("a")
	}
	if d := l.Attempt("b"); d != 0 {
		t.Errorf("key b refused: %v", d)
	}
}

func TestAttemptSweepsStaleKeysOncePerInterval(t *testing.T) {
	l := New()
	now := clock(l, t0)
	l.Attempt("stale")
	*now = t0.Add(window + time.Minute)
	l.Attempt("other")
	if _, ok := l.failures["stale"]; ok {
		t.Error("stale key kept after sweep")
	}
	l.Attempt("stale")
	*now = now.Add(time.Minute)
	l.Attempt("other")
	if _, ok := l.failures["stale"]; !ok {
		t.Error("swept again before the interval passed")
	}
}

func TestSweepKeepsBlockedClientBlocked(t *testing.T) {
	l := New()
	now := clock(l, t0)
	for range MaxFailures {
		l.Attempt("blocked")
	}
	*now = t0.Add(6 * time.Minute)
	l.Attempt("other")
	if d := l.Attempt("blocked"); d != 9*time.Minute {
		t.Errorf("Attempt = %v after sweep, want 9m", d)
	}
}

func TestAttemptCountsUntilSuccess(t *testing.T) {
	l := New()
	clock(l, t0)
	for i := range MaxFailures {
		if d := l.Attempt("ip"); d != 0 {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if d := l.Attempt("ip"); d != window {
		t.Errorf("Attempt = %v, want %v", d, window)
	}
	l.Success("ip")
	if d := l.Attempt("ip"); d != 0 {
		t.Errorf("refused after Success: %v", d)
	}
}

func TestParallelAttemptsPassAtMostMaxFailures(t *testing.T) {
	l := New()
	var passed atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if l.Attempt("ip") == 0 {
				passed.Add(1)
			}
		})
	}
	wg.Wait()
	if got := passed.Load(); got != MaxFailures {
		t.Errorf("%d of 50 parallel attempts passed, want %d", got, MaxFailures)
	}
}
