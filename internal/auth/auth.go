package auth

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
	"github.com/Wirezat/fileshare/internal/ttl"
)

const sessionTTL = 7 * 24 * time.Hour

var (
	ErrNotSetUp         = errors.New("auth: no admin password set")
	ErrWrongCredentials = errors.New("auth: wrong username or password")
	ErrWrongPassword    = errors.New("auth: current password is incorrect")
	ErrEmptyUsername    = errors.New("auth: username cannot be empty")
	ErrEmptyPassword    = errors.New("auth: password cannot be empty")
)

// RateLimitedError reports that a client sent too many wrong credentials.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e RateLimitedError) Error() string {
	return fmt.Sprintf("auth: too many attempts, retry in %s", e.RetryAfter)
}

// configStore must return from Current every Update that has already returned.
type configStore interface {
	Current() config.View
	Update(func(*config.Config) error) error
}

// Service checks admin credentials and keeps the admin sessions.
type Service struct {
	store    configStore
	sessions *ttl.Store[struct{}]
	limiter  *ratelimit.Limiter

	mu        sync.Mutex
	setupCode string
}

func New(store configStore) *Service {
	s := &Service{store: store, sessions: ttl.New[struct{}](), limiter: ratelimit.New()}
	s.mintSetupCode()
	return s
}

// NeedsSetup reports whether no admin password is set yet.
func (s *Service) NeedsSetup() bool {
	return s.store.Current().AdminPassword() == ""
}

// Login checks user and pass and returns a new session token; clientKey identifies the caller for rate limiting.
func (s *Service) Login(user, pass, clientKey string) (string, error) {
	cfg := s.store.Current()
	hash := cfg.AdminPassword()
	if hash == "" {
		return "", ErrNotSetUp
	}
	if wait := s.limiter.Attempt(clientKey); wait > 0 {
		return "", RateLimitedError{RetryAfter: wait}
	}
	passOK := password.Check(pass, hash)
	if user != cfg.AdminUsername() || !passOK {
		return "", ErrWrongCredentials
	}
	s.limiter.Success(clientKey)
	if password.NeedsRehash(hash) {
		hash = s.upgradeHash(hash, pass)
	}
	token, err := s.newSession()
	if err != nil {
		return "", err
	}
	if now := s.store.Current(); now.AdminUsername() != user || now.AdminPassword() != hash {
		s.sessions.Delete(token)
		return "", ErrWrongCredentials
	}
	return token, nil
}

// upgradeHash replaces the outdated admin hash old with an Argon2id hash of pass unless the password changed meanwhile, and returns the hash now expected in the store.
func (s *Service) upgradeHash(old, pass string) string {
	rehashed, err := password.Hash(pass)
	if err != nil {
		return old
	}
	stored := old
	err = s.store.Update(func(c *config.Config) error {
		if c.AdminPassword == old {
			c.AdminPassword = rehashed
			stored = rehashed
		}
		return nil
	})
	if err != nil {
		GoLog.Errorf("admin login: failed to store upgraded hash: %v", err)
		return old
	}
	if stored == rehashed {
		GoLog.Infof("admin password hash upgraded to argon2id")
	}
	return stored
}

func (s *Service) newSession() (string, error) {
	token, err := ttl.Token()
	if err != nil {
		return "", err
	}
	s.sessions.Set(token, struct{}{}, sessionTTL)
	return token, nil
}

// Valid reports whether token is a live session.
func (s *Service) Valid(token string) bool {
	_, ok := s.sessions.Get(token)
	return ok
}

// Logout ends the session token.
func (s *Service) Logout(token string) {
	s.sessions.Delete(token)
}

// ChangeUsername sets the admin username to name if current is the admin password, then ends every session but keep; it returns how many it ended.
func (s *Service) ChangeUsername(current, name, keep string) (int, error) {
	if name == "" {
		return 0, ErrEmptyUsername
	}
	if err := s.checkCurrent(current, keep); err != nil {
		return 0, err
	}
	err := s.store.Update(func(c *config.Config) error {
		c.AdminUsername = name
		return nil
	})
	if err != nil {
		return 0, err
	}
	return s.sessions.DeleteExcept(keep), nil
}

// ChangePassword sets the admin password to next if current is the admin password, then ends every session but keep; it returns how many it ended.
func (s *Service) ChangePassword(current, next, keep string) (int, error) {
	if next == "" {
		return 0, ErrEmptyPassword
	}
	if err := s.checkCurrent(current, keep); err != nil {
		return 0, err
	}
	hashed, err := password.Hash(next)
	if err != nil {
		return 0, err
	}
	err = s.store.Update(func(c *config.Config) error {
		c.AdminPassword = hashed
		return nil
	})
	if err != nil {
		return 0, err
	}
	return s.sessions.DeleteExcept(keep), nil
}

func (s *Service) checkCurrent(current, session string) error {
	key := "change|" + session
	if wait := s.limiter.Attempt(key); wait > 0 {
		return RateLimitedError{RetryAfter: wait}
	}
	if !password.Check(current, s.store.Current().AdminPassword()) {
		return ErrWrongPassword
	}
	s.limiter.Success(key)
	return nil
}
