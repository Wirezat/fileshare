package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
)

var (
	ErrSetupDone      = errors.New("auth: setup already complete")
	ErrWrongSetupCode = errors.New("auth: wrong setup code")
)

func (s *Service) mintSetupCode() {
	if !s.NeedsSetup() {
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return
	}
	s.mu.Lock()
	s.setupCode = hex.EncodeToString(b)
	s.mu.Unlock()
}

// SetupCode returns the one-shot setup code, or "" when setup is done or no code could be made.
func (s *Service) SetupCode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupCode
}

// Setup sets the first admin password, and the username unless user is empty, if code is the setup code.
func (s *Service) Setup(code, user, pass, clientKey string) error {
	if !s.NeedsSetup() {
		return ErrSetupDone
	}
	key := "setup|" + clientKey
	if wait := s.limiter.Attempt(key); wait > 0 {
		return RateLimitedError{RetryAfter: wait}
	}
	want := s.SetupCode()
	if want == "" || subtle.ConstantTimeCompare([]byte(code), []byte(want)) != 1 {
		return ErrWrongSetupCode
	}
	s.limiter.Success(key)
	if pass == "" {
		return ErrEmptyPassword
	}
	hashed, err := password.Hash(pass)
	if err != nil {
		return err
	}
	err = s.store.Update(func(c *config.Config) error {
		if c.AdminPassword != "" {
			return ErrSetupDone
		}
		if user != "" {
			c.AdminUsername = user
		}
		c.AdminPassword = hashed
		return nil
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.setupCode = ""
	s.mu.Unlock()
	return nil
}
