package share

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/ttl"
)

const tokenTTL = 24 * time.Hour

var (
	ErrNotFound   = errors.New("share: not found")
	ErrOutside    = errors.New("share: path leaves the share")
	ErrExpired    = errors.New("share: expired")
	ErrLocked     = errors.New("share: password required")
	ErrUploadsOff = errors.New("share: uploads not allowed")
)

type configStore interface {
	Current() config.View
	Update(func(*config.Config) error) error
}

// Service applies the share rules on top of the config store.
type Service struct {
	store  configStore
	tokens *ttl.Store[string]
}

func New(store configStore) *Service {
	return &Service{store: store, tokens: ttl.New[string]()}
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

// Grant issues a token that unlocks subpath for tokenTTL.
func (s *Service) Grant(subpath string) (string, error) {
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
