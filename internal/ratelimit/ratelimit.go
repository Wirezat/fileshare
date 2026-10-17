package ratelimit

import (
	"sync"
	"time"
)

const (
	MaxFailures   = 5
	window        = 15 * time.Minute
	sweepInterval = 5 * time.Minute
)

// Limiter refuses a key after MaxFailures failures inside window until the oldest one ages out.
type Limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	swept    time.Time
	now      func() time.Time
}

func New() *Limiter {
	return &Limiter{failures: map[string][]time.Time{}, now: time.Now}
}

// Attempt returns how long key must wait, or zero after counting this attempt as a failure until Success clears it.
func (l *Limiter) Attempt(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if recent := l.prune(key, now); len(recent) >= MaxFailures {
		return recent[0].Add(window).Sub(now)
	}
	l.record(key, now)
	return 0
}

func (l *Limiter) record(key string, now time.Time) {
	if now.Sub(l.swept) > sweepInterval {
		for k := range l.failures {
			l.prune(k, now)
		}
		l.swept = now
	}
	l.failures[key] = append(l.prune(key, now), now)
}

func (l *Limiter) Success(key string) {
	l.mu.Lock()
	delete(l.failures, key)
	l.mu.Unlock()
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-window)
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
