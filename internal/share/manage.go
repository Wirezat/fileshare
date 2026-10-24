package share

import (
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
)

var ErrExists = errors.New("share: subpath already exists")

// InvalidError is a request the share rules refuse; its text can be shown to the admin.
type InvalidError string

func (e InvalidError) Error() string { return string(e) }

// Create stores a new share at subpath with a cleaned path, a hashed password and the current upload time.
func (s *Service) Create(subpath string, fd config.FileData) error {
	if subpath == "" || fd.Path == "" {
		return InvalidError("subpath and path are required")
	}
	if Reserved(subpath) {
		return InvalidError("subpath is reserved")
	}
	if !config.ValidOffice(fd.Office) {
		return InvalidError("office must be empty, view or edit")
	}
	if fd.Password != "" {
		hashed, err := password.Hash(fd.Password)
		if err != nil {
			return err
		}
		fd.Password = hashed
	}
	fd.Path = filepath.Clean(fd.Path)
	if p := problemOf(fd.Path, fd.AllowPost); p != "" {
		return InvalidError(p)
	}
	fd.UploadTime = time.Now().Unix()
	return s.store.Update(func(c *config.Config) error {
		if _, exists := c.Files[subpath]; exists {
			return ErrExists
		}
		c.Files[subpath] = fd
		return nil
	})
}

// Patch lists the share fields an admin may change; nil fields stay as they are, an empty Password removes it.
type Patch struct {
	Path       *string `json:"path"`
	Expiration *int64  `json:"expiration"`
	AllowPost  *bool   `json:"allow_post"`
	NoZip      *bool   `json:"no_zip"`
	Office     *string `json:"office"`
	Expired    *bool   `json:"expired"`
	Password   *string `json:"password"`
}

// Patch applies the set fields of p to the share at subpath and returns one "field -> value" line per change.
func (s *Service) Patch(subpath string, p Patch) ([]string, error) {
	cur, ok := s.store.Current().Share(subpath)
	if !ok {
		return nil, ErrNotFound
	}

	var changes []string
	var apply []func(*config.FileData)
	set := func(field, value string, fn func(*config.FileData)) {
		changes = append(changes, field+" -> "+value)
		apply = append(apply, fn)
	}

	if p.Path != nil {
		if *p.Path == "" {
			return nil, InvalidError("path cannot be empty")
		}
		cleaned := filepath.Clean(*p.Path)
		set("path", cleaned, func(fd *config.FileData) { fd.Path = cleaned })
	}
	if p.Expiration != nil {
		set("expiration", strconv.FormatInt(*p.Expiration, 10), func(fd *config.FileData) { fd.Expiration = *p.Expiration })
	}
	if p.AllowPost != nil {
		set("allow_post", strconv.FormatBool(*p.AllowPost), func(fd *config.FileData) { fd.AllowPost = *p.AllowPost })
	}
	if p.NoZip != nil {
		set("no_zip", strconv.FormatBool(*p.NoZip), func(fd *config.FileData) { fd.NoZip = *p.NoZip })
	}
	if p.Office != nil {
		if !config.ValidOffice(*p.Office) {
			return nil, InvalidError("office must be empty, view or edit")
		}
		set("office", *p.Office, func(fd *config.FileData) { fd.Office = *p.Office })
	}
	if p.Expired != nil {
		set("expired", strconv.FormatBool(*p.Expired), func(fd *config.FileData) { fd.Expired = *p.Expired })
	}
	if p.Password != nil {
		if *p.Password == "" {
			set("password", "<removed>", func(fd *config.FileData) { fd.Password = "" })
		} else {
			hashed, err := password.Hash(*p.Password)
			if err != nil {
				return nil, err
			}
			set("password", "***", func(fd *config.FileData) { fd.Password = hashed })
		}
	}
	if len(changes) == 0 {
		return nil, InvalidError("no fields to update")
	}
	if p.Path != nil || (p.AllowPost != nil && *p.AllowPost) {
		path, allowPost := cur.Path, cur.AllowPost
		if p.Path != nil {
			path = filepath.Clean(*p.Path)
		}
		if p.AllowPost != nil {
			allowPost = *p.AllowPost
		}
		if problem := problemOf(path, allowPost); problem != "" {
			return nil, InvalidError(problem)
		}
	}

	err := s.store.Update(func(c *config.Config) error {
		fd, ok := c.Files[subpath]
		if !ok {
			return ErrNotFound
		}
		for _, fn := range apply {
			fn(&fd)
		}
		c.Files[subpath] = fd
		return nil
	})
	if err != nil {
		return nil, err
	}
	if p.Password != nil {
		s.revoke(subpath)
	}
	return changes, nil
}

// Delete removes the share at subpath with its unlock tokens and returns it; an unknown subpath is not an error.
func (s *Service) Delete(subpath string) (config.FileData, error) {
	var removed config.FileData
	err := s.store.Update(func(c *config.Config) error {
		removed = c.Files[subpath]
		delete(c.Files, subpath)
		return nil
	})
	if err == nil {
		s.revoke(subpath)
	}
	return removed, err
}

// Prune removes every expired share and returns how many it removed.
func (s *Service) Prune() (int, error) {
	pruned := 0
	err := s.store.Update(func(c *config.Config) error {
		for subpath, fd := range c.Files {
			if config.IsExpired(fd) {
				delete(c.Files, subpath)
				pruned++
			}
		}
		return nil
	})
	return pruned, err
}
