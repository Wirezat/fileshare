package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/Wirezat/fileshare/internal/config"
)

// FileStore holds the configuration in memory and persists every change to one JSON file.
type FileStore struct {
	path string
	mu   sync.Mutex
	cur  atomic.Pointer[config.Config]
}

// Open loads the config at path; the file must exist and be valid JSON.
func Open(path string) (*FileStore, error) {
	cfg, err := load(path)
	if err != nil {
		return nil, err
	}
	s := &FileStore{path: path}
	s.cur.Store(cfg)
	return s, nil
}

// Current returns a read-only view of the latest saved config.
func (s *FileStore) Current() config.View {
	return config.NewView(s.cur.Load())
}

// Update applies fn to a copy of the config, saves it and then makes it current.
// If fn or saving fails, nothing changes and the error is returned.
func (s *FileStore) Update(fn func(*config.Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cur.Load().Clone()
	if err := fn(next); err != nil {
		return err
	}
	if err := save(s.path, next); err != nil {
		return err
	}
	s.cur.Store(next)
	return nil
}

func load(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config: %w", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	cfg.ApplyDefaults()
	return &cfg, nil
}

func save(path string, cfg *config.Config) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".data.json.tmp*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("failed to encode config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	return nil
}
