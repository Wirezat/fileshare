// Package stats counts downloads, uploads and visitors in memory and keeps the counts in stats.json.
package stats

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
)

const (
	window    = 30
	saveDelay = 5 * time.Second
)

// Stats records events and writes them to its file at most every saveDelay. It is safe for concurrent use.
type Stats struct {
	path  string
	now   func() time.Time
	loc   *time.Location
	delay time.Duration

	mu      sync.Mutex
	f       *file
	today   string
	live    int
	uploads map[string]*uploadRun
	timer   *time.Timer
	closed  bool

	writeMu sync.Mutex
}

// Open loads the stats kept at path. A missing file starts the count now; an unreadable one is
// moved to path.broken-<time>, logged, and the count starts over.
func Open(path string) *Stats {
	return open(path, time.Now, time.Local)
}

func open(path string, now func() time.Time, loc *time.Location) *Stats {
	s := &Stats{
		path:    path,
		now:     now,
		loc:     loc,
		delay:   saveDelay,
		uploads: map[string]*uploadRun{},
	}
	s.f = s.load()
	return s
}

func (s *Stats) load() *file {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return newFile(s.now(), newKey())
	}
	if err == nil {
		var f file
		if err = json.Unmarshal(data, &f); err == nil {
			if err = f.check(); err == nil {
				return &f
			}
		}
	}
	aside := s.path + ".broken-" + s.now().Format("20060102-150405")
	if rerr := os.Rename(s.path, aside); rerr != nil {
		GoLog.Errorf("stats: %s is unreadable (%v) and could not be moved aside: %v; starting over", s.path, err, rerr)
	} else {
		GoLog.Errorf("stats: %s is unreadable (%v), moved to %s; starting over", s.path, err, aside)
	}
	return newFile(s.now(), newKey())
}

func newKey() []byte {
	key := make([]byte, keySize)
	rand.Read(key)
	return key
}

// hash returns the keyed 64-bit hash of a client address.
func (s *Stats) hash(client string) uint64 {
	mac := hmac.New(sha256.New, s.f.Key)
	mac.Write([]byte(client))
	return binary.BigEndian.Uint64(mac.Sum(nil))
}

// changed schedules a save; the caller holds s.mu.
func (s *Stats) changed() {
	if s.timer == nil && !s.closed {
		s.timer = time.AfterFunc(s.delay, func() {
			if err := s.save(); err != nil {
				GoLog.Errorf("stats: %v", err)
			}
		})
	}
}

// Close writes pending changes and stops further scheduled saves.
func (s *Stats) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	return s.save()
}

// save writes the current state atomically through a temporary file next to path.
func (s *Stats) save() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	s.timer = nil
	snapshot := s.f.clone()
	s.mu.Unlock()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", s.path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	return nil
}
