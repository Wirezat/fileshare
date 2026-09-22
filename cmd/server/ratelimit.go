package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
)

// Sliding-window failure limiter for password checks: after maxFailures inside
// failureWindow a key is refused until the oldest failure ages out.

const (
	maxFailures    = 5
	failureWindow  = 15 * time.Minute
	limiterReapInt = 5 * time.Minute
)

type limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLimiter() *limiter {
	return &limiter{failures: map[string][]time.Time{}}
}

var (
	loginLimiter  = newLimiter()
	unlockLimiter = newLimiter()
)

// retryAfter reports how long the key must wait, or zero if it may proceed.
func (l *limiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.prune(key, time.Now())
	if len(recent) < maxFailures {
		return 0
	}
	return time.Until(recent[0].Add(failureWindow))
}

func (l *limiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.failures[key] = append(l.prune(key, now), now)
}

func (l *limiter) recordSuccess(key string) {
	l.mu.Lock()
	delete(l.failures, key)
	l.mu.Unlock()
}

// prune drops failures that have aged out. Caller holds the lock.
func (l *limiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-failureWindow)
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
	return kept
}

// allow writes a 429 and reports false when the key is out of attempts.
// what names the endpoint for the log line.
func (l *limiter) allow(w http.ResponseWriter, key, what string) bool {
	wait := l.retryAfter(key)
	if wait <= 0 {
		return true
	}
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
	GoLog.Warnf("%s: rate limited %s for %ds", what, key, secs)
	return false
}

func startLimiterReaper() {
	runEvery(limiterReapInt, func() {
		now := time.Now()
		for _, l := range []*limiter{loginLimiter, unlockLimiter} {
			l.mu.Lock()
			for key := range l.failures {
				l.prune(key, now)
			}
			l.mu.Unlock()
		}
	})
}
