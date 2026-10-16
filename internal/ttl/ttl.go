package ttl

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const sweepInterval = 5 * time.Minute

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

// Store maps tokens to values that expire after their own TTL.
type Store[V any] struct {
	mu      sync.RWMutex
	entries map[string]entry[V]
	swept   time.Time
}

func New[V any]() *Store[V] {
	return &Store[V]{entries: map[string]entry[V]{}}
}

// Set stores value under token for ttl and drops expired entries at most once per sweepInterval.
func (s *Store[V]) Set(token string, value V, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.swept) > sweepInterval {
		for k, e := range s.entries {
			if now.After(e.expiresAt) {
				delete(s.entries, k)
			}
		}
		s.swept = now
	}
	s.entries[token] = entry[V]{value: value, expiresAt: now.Add(ttl)}
}

// Get returns the value for token and false if it is missing or expired.
func (s *Store[V]) Get(token string) (V, bool) {
	s.mu.RLock()
	e, ok := s.entries[token]
	s.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (s *Store[V]) Delete(token string) {
	s.mu.Lock()
	delete(s.entries, token)
	s.mu.Unlock()
}

// DeleteExcept drops every entry but keep and returns how many it removed.
func (s *Store[V]) DeleteExcept(keep string) int {
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

func (s *Store[V]) DeleteFunc(match func(token string, value V) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for token, e := range s.entries {
		if match(token, e.value) {
			delete(s.entries, token)
			removed++
		}
	}
	return removed
}

// Token returns a random 48-character hex string for session and share tokens.
func Token() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
