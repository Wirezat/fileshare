package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
)

// Failure-counting limiter for the two places that verify a password.
//
// Password checks are deliberately slow — Argon2id at 64 MiB takes tens of
// milliseconds — but "slow" only raises the cost of guessing, it does not cap
// it. A few dozen guesses per second, sustained, is enough to walk a weak share
// password. This puts a ceiling on attempts per source instead.
//
// The window is sliding and per key: after maxFailures inside failureWindow the
// key is refused until the oldest failure ages out. A success clears the key,
// so ordinary use never accumulates.

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
	// Blocked until the oldest failure in the window expires.
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
	go func() {
		ticker := time.NewTicker(limiterReapInt)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			for _, l := range []*limiter{loginLimiter, unlockLimiter} {
				l.mu.Lock()
				for key := range l.failures {
					l.prune(key, now)
				}
				l.mu.Unlock()
			}
		}
	}()
}
