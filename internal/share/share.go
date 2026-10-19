package share

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
	"github.com/Wirezat/fileshare/internal/ttl"
)

const tokenTTL = 24 * time.Hour

var (
	ErrNotFound      = errors.New("share: not found")
	ErrOutside       = errors.New("share: path leaves the share")
	ErrExpired       = errors.New("share: expired")
	ErrLocked        = errors.New("share: password required")
	ErrUploadsOff    = errors.New("share: uploads not allowed")
	ErrNoPassword    = errors.New("share: no password set")
	ErrWrongPassword = errors.New("share: wrong password")
)

// RateLimitedError reports that a client sent too many wrong passwords for a share.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e RateLimitedError) Error() string {
	return fmt.Sprintf("share: too many attempts, retry in %s", e.RetryAfter)
}

type configStore interface {
	Current() config.View
	Update(func(*config.Config) error) error
	UpdateIfAccepted(func(*config.Config) error) error
	OnOutsideChange(func(old, next config.View))
}

type Service struct {
	store   configStore
	tokens  *ttl.Store[string]
	limiter *ratelimit.Limiter
}

func New(store configStore) *Service {
	s := &Service{store: store, tokens: ttl.New[string](), limiter: ratelimit.New()}
	store.OnOutsideChange(s.outsideChange)
	return s
}

func (s *Service) outsideChange(old, next config.View) {
	for subpath, before := range old.Shares() {
		after, ok := next.Share(subpath)
		if !ok || after.Password != before.Password || after.Path != before.Path {
			s.revoke(subpath)
			GoLog.Infof("share /%s changed in data.json, its unlocks ended", subpath)
		}
	}
}

// Access is what a request presents to open a password-protected share.
type Access struct {
	Token      string
	AuthHeader string
	RequestURI string
}

// Resolved is a share and the path a request points at inside it.
type Resolved struct {
	Config   config.View
	Subpath  string
	Share    config.FileData
	DiskPath string
}

// Join returns root joined with rel and false if the result leaves root.
func Join(root, rel string) (string, bool) {
	p := filepath.Join(root, rel)
	return p, p == root || strings.HasPrefix(p, root+"/")
}

// Resolve finds the share at subpath and the path rel inside it, then checks containment, expiry and the password, in that order.
func (s *Service) Resolve(subpath, rel string, a Access) (Resolved, error) {
	cfg := s.store.Current()
	fd, ok := cfg.Share(subpath)
	if !ok {
		return Resolved{}, ErrNotFound
	}
	p, inside := Join(fd.Path, rel)
	res := Resolved{Config: cfg, Subpath: subpath, Share: fd, DiskPath: p}
	if !inside {
		return res, ErrOutside
	}
	if config.IsExpired(fd) {
		return res, ErrExpired
	}
	if fd.Password != "" && !s.TokenValid(a.Token, subpath) && !office.TokenAllows(a.AuthHeader, a.RequestURI, cfg.OfficeSecret()) {
		return res, ErrLocked
	}
	return res, nil
}

// TokenValid reports whether token unlocks subpath.
func (s *Service) TokenValid(token, subpath string) bool {
	sp, ok := s.tokens.Get(token)
	return ok && sp == subpath
}

// Unlock checks pw against the share at subpath and returns a token that unlocks it; clientKey identifies the caller for rate limiting.
func (s *Service) Unlock(subpath, pw, clientKey string) (string, error) {
	fd, ok := s.store.Current().Share(subpath)
	if !ok {
		return "", ErrNotFound
	}
	if fd.Password == "" {
		return "", ErrNoPassword
	}
	key := clientKey + "|" + subpath
	if wait := s.limiter.Attempt(key); wait > 0 {
		return "", RateLimitedError{RetryAfter: wait}
	}
	if !password.Check(pw, fd.Password) {
		return "", ErrWrongPassword
	}
	s.limiter.Success(key)
	hash := fd.Password
	if password.NeedsRehash(hash) {
		hash = s.upgradeHash(subpath, hash, pw)
	}
	token, err := s.grant(subpath)
	if err != nil {
		return "", err
	}
	if now, ok := s.store.Current().Share(subpath); !ok || now.Password != hash || now.Path != fd.Path {
		s.tokens.Delete(token)
		return "", ErrWrongPassword
	}
	return token, nil
}

// upgradeHash replaces the outdated hash old of subpath with an Argon2id hash of pw unless the password changed meanwhile, and returns the hash now expected in the store.
func (s *Service) upgradeHash(subpath, old, pw string) string {
	rehashed, err := password.Hash(pw)
	if err != nil {
		return old
	}
	stored := old
	err = s.store.UpdateIfAccepted(func(c *config.Config) error {
		if cur, ok := c.Files[subpath]; ok && cur.Password == old {
			cur.Password = rehashed
			c.Files[subpath] = cur
			stored = rehashed
		}
		return nil
	})
	if errors.Is(err, config.ErrRefused) {
		GoLog.Infof("share unlock: hash upgrade for /%s postponed while data.json is not taken over", subpath)
		return old
	}
	if err != nil {
		GoLog.Errorf("share unlock: failed to store upgraded hash: %v", err)
		return old
	}
	if stored == rehashed {
		GoLog.Infof("share password hash upgraded to argon2id: /%s", subpath)
	}
	return stored
}

func (s *Service) revoke(subpath string) {
	s.tokens.DeleteFunc(func(_ string, sp string) bool { return sp == subpath })
}

func (s *Service) grant(subpath string) (string, error) {
	token, err := ttl.Token()
	if err != nil {
		return "", err
	}
	s.tokens.Set(token, subpath, tokenTTL)
	return token, nil
}

// UploadTarget returns the share at subpath if it accepts uploads.
func (s *Service) UploadTarget(subpath string) (config.FileData, error) {
	fd, ok := s.store.Current().Share(subpath)
	switch {
	case !ok:
		return fd, ErrNotFound
	case !fd.AllowPost:
		return fd, ErrUploadsOff
	case config.IsExpired(fd):
		return fd, ErrExpired
	}
	return fd, nil
}
