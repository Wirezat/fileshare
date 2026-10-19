package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
)

const checkInterval = 2 * time.Second

// FileStore holds the configuration in memory, persists every change to one JSON file and picks up outside edits of that file.
type FileStore struct {
	path         string
	mu           sync.Mutex
	cur          atomic.Pointer[config.Config]
	seen         os.FileInfo
	seenSum      [sha256.Size]byte
	lastRevision int64
	start        time.Time
	broken       os.FileInfo
	brokenErr    string
	nextCheck    atomic.Int64
	now          func() time.Time
	stat         func(string) (os.FileInfo, error)
	createTemp   func(dir, pattern string) (*os.File, error)
	rename       func(from, to string) error
	listeners    []func(old, next config.View)
}

type change struct{ old, next *config.Config }

// Open loads the config at path; the file must exist and be valid JSON.
func Open(path string) (*FileStore, error) {
	cfg, sum, err := load(path)
	if err != nil {
		return nil, err
	}
	s := &FileStore{path: path, now: time.Now, stat: os.Stat, createTemp: os.CreateTemp, rename: os.Rename}
	s.start = s.now()
	s.cur.Store(cfg)
	s.seen, _ = s.stat(path)
	s.seenSum, s.lastRevision = sum, cfg.Revision
	s.nextCheck.Store(int64(checkInterval))
	return s, nil
}

func (s *FileStore) elapsed() int64 {
	return int64(s.now().Sub(s.start))
}

// Current returns a read-only view of the latest config, rereading the file first if checkInterval has passed and it changed.
func (s *FileStore) Current() config.View {
	now := s.elapsed()
	if due := s.nextCheck.Load(); now >= due && s.nextCheck.CompareAndSwap(due, now+int64(checkInterval)) {
		s.mu.Lock()
		ch, _ := s.refresh()
		s.mu.Unlock()
		s.notify(ch)
	}
	return config.NewView(s.cur.Load())
}

// Refused returns why the file on disk was not taken over at the last check, or "" if it was.
func (s *FileStore) Refused() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.brokenErr
}

// OnOutsideChange registers fn to run after an edit of the file from outside the server has become current; fn gets the state before and after.
func (s *FileStore) OnOutsideChange(fn func(old, next config.View)) {
	s.mu.Lock()
	s.listeners = append(s.listeners, fn)
	s.mu.Unlock()
}

func (s *FileStore) notify(ch *change) {
	if ch == nil {
		return
	}
	s.mu.Lock()
	fns := append([]func(old, next config.View){}, s.listeners...)
	s.mu.Unlock()
	for _, fn := range fns {
		fn(config.NewView(ch.old), config.NewView(ch.next))
	}
}

// Update applies fn to a copy of the config, saves it and then makes it current.
// If fn or saving fails, nothing changes and the error is returned.
func (s *FileStore) Update(fn func(*config.Config) error) error {
	ch, err := s.update(fn, false)
	s.notify(ch)
	return err
}

// UpdateIfAccepted works like Update, but while the file on disk is not taken over it changes nothing and returns an error wrapping config.ErrRefused.
func (s *FileStore) UpdateIfAccepted(fn func(*config.Config) error) error {
	ch, err := s.update(fn, true)
	s.notify(ch)
	return err
}

func (s *FileStore) update(fn func(*config.Config) error, onlyAccepted bool) (*change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, refused := s.refresh()
	if refused != nil && onlyAccepted {
		return ch, fmt.Errorf("%w: %v", config.ErrRefused, refused)
	}
	cur := s.cur.Load()
	next := cur.Clone()
	if err := fn(next); err != nil {
		return ch, err
	}
	if reflect.DeepEqual(cur, next) {
		return ch, nil
	}
	next.Revision = s.lastRevision + 1
	tmp, saved, sum, err := s.writeTemp(next)
	if err != nil {
		return ch, err
	}
	aside := ""
	if refused != nil {
		if aside, err = s.setAside(); err != nil {
			os.Remove(tmp)
			return ch, err
		}
	}
	if err := s.rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		if aside != "" {
			if back := s.rename(aside, s.path); back != nil {
				GoLog.Errorf("config: data.json is missing, %s could not be moved back: %v", aside, back)
			}
		}
		return ch, fmt.Errorf("failed to replace config file: %w", err)
	}
	switch {
	case aside != "":
		GoLog.Warnf("config: data.json not taken over (%v), moved it to %s and wrote the server's state", refused, aside)
	case refused != nil:
		GoLog.Warnf("config: data.json was missing, wrote the server's state")
	}
	s.cur.Store(next)
	s.seen, s.seenSum, s.lastRevision = saved, sum, next.Revision
	s.broken, s.brokenErr = nil, ""
	return ch, nil
}

// setAside moves the file that was not taken over to data.json.rejected-<time> and returns that name, or "" if the file is missing.
func (s *FileStore) setAside() (string, error) {
	if _, err := s.stat(s.path); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	aside := s.path + ".rejected-" + s.now().Format("20060102-150405.000000")
	if err := s.rename(s.path, aside); err != nil {
		return "", fmt.Errorf("store: data.json not taken over and not set aside: %w", err)
	}
	return aside, nil
}

// refresh rereads the file if it differs from the state cur was loaded from and returns that change; on failure cur stays and the error is logged once per file state.
func (s *FileStore) refresh() (*change, error) {
	fi, err := s.stat(s.path)
	if err == nil && sameFile(fi, s.seen) {
		return nil, nil
	}
	var cfg *config.Config
	var sum [sha256.Size]byte
	if err == nil {
		cfg, sum, err = load(s.path)
	}
	if err == nil && sum == s.seenSum {
		s.seen, s.broken, s.brokenErr = fi, nil, ""
		return nil, nil
	}
	if err == nil && cfg.Revision < s.lastRevision {
		err = fmt.Errorf("data.json has revision %d, older than the server's last save (%d)", cfg.Revision, s.lastRevision)
	}
	if err != nil {
		repeat := err.Error() == s.brokenErr && (fi == nil && s.broken == nil || sameFile(fi, s.broken))
		if !repeat {
			GoLog.Errorf("config: data.json not taken over, keeping the last good state: %v", err)
		}
		s.broken, s.brokenErr = fi, err.Error()
		return nil, err
	}
	old := s.cur.Swap(cfg)
	s.seen, s.seenSum, s.lastRevision, s.broken, s.brokenErr = fi, sum, cfg.Revision, nil, ""
	GoLog.Infof("config: data.json changed outside the server, reloaded")
	return &change{old: old, next: cfg}, nil
}

func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func load(path string) (*config.Config, [sha256.Size]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("failed to open config: %w", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("failed to decode config: %w", err)
	}
	cfg.ApplyDefaults()
	return &cfg, sha256.Sum256(data), nil
}

// writeTemp writes cfg to a closed temp file next to the config file and returns its name, its file info and the SHA-256 of its content.
func (s *FileStore) writeTemp(cfg *config.Config) (string, os.FileInfo, [sha256.Size]byte, error) {
	var none [sha256.Size]byte
	tmp, err := s.createTemp(filepath.Dir(s.path), ".data.json.tmp*")
	if err != nil {
		return "", nil, none, fmt.Errorf("failed to create temp file: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err == nil {
		data = append(data, '\n')
		_, err = tmp.Write(data)
	}
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, none, fmt.Errorf("failed to encode config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, none, fmt.Errorf("failed to close temp file: %w", err)
	}
	written, err := os.Stat(tmp.Name())
	if err != nil {
		os.Remove(tmp.Name())
		return "", nil, none, fmt.Errorf("failed to stat temp file: %w", err)
	}
	return tmp.Name(), written, sha256.Sum256(data), nil
}
