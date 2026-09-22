package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// runEvery calls fn on every tick of interval, forever, in its own goroutine.
func runEvery(interval time.Duration, fn func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			fn()
		}
	}()
}

// generateToken returns a random 48-character hex string suitable for
// session and share tokens.
func generateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type ttlEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// ttlStore is an in-memory map of tokens to values that expire after their
// own TTL. A reaper goroutine, started separately via startReaper, sweeps
// expired entries; get also checks expiry so callers are correct even
// before the first sweep.
type ttlStore[V any] struct {
	mu      sync.RWMutex
	entries map[string]ttlEntry[V]
}

func newTTLStore[V any]() *ttlStore[V] {
	return &ttlStore[V]{entries: map[string]ttlEntry[V]{}}
}

func (s *ttlStore[V]) set(token string, value V, ttl time.Duration) {
	s.mu.Lock()
	s.entries[token] = ttlEntry[V]{value: value, expiresAt: time.Now().Add(ttl)}
	s.mu.Unlock()
}

func (s *ttlStore[V]) get(token string) (V, bool) {
	s.mu.RLock()
	e, ok := s.entries[token]
	s.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (s *ttlStore[V]) delete(token string) {
	s.mu.Lock()
	delete(s.entries, token)
	s.mu.Unlock()
}

// deleteExcept drops every entry but the one given and reports how many it removed.
func (s *ttlStore[V]) deleteExcept(keep string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for token := range s.entries {
		if token != keep {
			delete(s.entries, token)
			removed++
		}
	}
	return removed
}

// startReaper periodically removes expired entries.
func (s *ttlStore[V]) startReaper(interval time.Duration) {
	runEvery(interval, func() {
		now := time.Now()
		s.mu.Lock()
		for token, e := range s.entries {
			if now.After(e.expiresAt) {
				delete(s.entries, token)
			}
		}
		s.mu.Unlock()
	})
}
