package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

const chunkTempBase = "/tmp/fileshare-chunks"

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// Storage is the interface for chunked file uploads.
type Storage interface {
	InitChunk(uploadID, filename string, totalChunks int, destDir string) (missingChunks []int, err error)
	ReceiveChunk(uploadID string, index int, r io.Reader) (done bool, err error)
	SetInactivityTimeout(d time.Duration)
}

// sessionMeta is persisted as meta.json inside each chunk directory.
// It allows resume across server restarts.
type sessionMeta struct {
	UploadID     string    `json:"uploadId"`
	Filename     string    `json:"filename"`
	TotalChunks  int       `json:"totalChunks"`
	DestDir      string    `json:"destDir"`
	Received     []int     `json:"received"`
	LastActivity time.Time `json:"lastActivity"`
}

type chunkSession struct {
	meta     sessionMeta
	mu       sync.Mutex
	received map[int]struct{}
}

// LocalStorage saves assembled uploads to the local filesystem.
type LocalStorage struct {
	mu                sync.RWMutex
	inactivityTimeout time.Duration
}

var (
	sessionsMu sync.RWMutex
	sessions   = map[string]*chunkSession{}
)

func NewLocalStorage(cfg *shared.Config) *LocalStorage {
	return &LocalStorage{
		inactivityTimeout: time.Duration(cfg.ChunkInactivityTimeout) * time.Second,
	}
}

func (s *LocalStorage) SetInactivityTimeout(d time.Duration) {
	s.mu.Lock()
	s.inactivityTimeout = d
	s.mu.Unlock()
}

// InitChunk registers or resumes an upload session.
// uploadID is provided by the client (deterministic hash of filename+size+lastModified).
// Returns the list of chunk indices still missing so the client can skip already-uploaded chunks.
func (s *LocalStorage) InitChunk(uploadID, filename string, totalChunks int, destDir string) ([]int, error) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	sess, err := s.loadOrCreateSession(uploadID, filename, totalChunks, destDir)
	if err != nil {
		return nil, err
	}

	sess.mu.Lock()
	missing := missingChunks(sess.received, sess.meta.TotalChunks)
	sess.mu.Unlock()

	GoLog.Debugf("chunk upload init/resume: id=%q missing=%d", uploadID, len(missing))
	return missing, nil
}

// loadOrCreateSession checks for an existing upload  session in RAM or on disk, or creates a new one if not found.
func (s *LocalStorage) loadOrCreateSession(uploadID, filename string, totalChunks int, destDir string) (*chunkSession, error) {
	// 1. RAM hit
	if sess, ok := sessions[uploadID]; ok {
		return sess, nil
	}

	// 2. Disk hit
	dir := filepath.Join(chunkTempBase, uploadID)
	if data, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("corrupt meta.json for %q: %w", uploadID, err)
		}
		received := make(map[int]struct{}, len(meta.Received))
		for _, i := range meta.Received {
			received[i] = struct{}{}
		}
		sess := &chunkSession{meta: meta, received: received}
		sessions[uploadID] = sess
		return sess, nil
	}

	// 3. New Session
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating temp dir for %q: %w", uploadID, err)
	}
	meta := sessionMeta{
		UploadID:     uploadID,
		Filename:     sanitizeFilename(filename),
		TotalChunks:  totalChunks,
		DestDir:      destDir,
		Received:     []int{},
		LastActivity: time.Now(),
	}
	if err := writeMeta(dir, meta); err != nil {
		return nil, err
	}
	sess := &chunkSession{meta: meta, received: make(map[int]struct{})}
	sessions[uploadID] = sess
	return sess, nil
}

// ReceiveChunk stores a single chunk and updates meta.json.
// Returns done=true when all chunks have arrived and the file has been assembled.
func (s *LocalStorage) ReceiveChunk(uploadID string, index int, r io.Reader) (bool, error) {
	sessionsMu.RLock()
	sess, ok := sessions[uploadID]
	sessionsMu.RUnlock()
	if !ok {
		return false, fmt.Errorf("unknown upload session %q", uploadID)
	}
	if index < 0 || index >= sess.meta.TotalChunks {
		return false, fmt.Errorf("chunk index %d out of range [0, %d)", index, sess.meta.TotalChunks)
	}

	chunkPath := filepath.Join(chunkTempBase, uploadID, fmt.Sprintf("%05d", index))
	f, err := os.Create(chunkPath)
	if err != nil {
		return false, fmt.Errorf("creating chunk file: %w", err)
	}
	buf := bufPool.Get().(*[]byte)
	_, err = io.CopyBuffer(f, r, *buf)
	bufPool.Put(buf)
	if err != nil {
		f.Close()
		os.Remove(chunkPath)
		return false, fmt.Errorf("writing chunk %d: %w", index, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(chunkPath)
		return false, fmt.Errorf("closing chunk %d: %w", index, err)
	}

	sess.mu.Lock()
	sess.received[index] = struct{}{}
	sess.meta.LastActivity = time.Now()
	sess.meta.Received = toSlice(sess.received)
	done := len(sess.received) == sess.meta.TotalChunks
	metaSnap := sess.meta
	sess.mu.Unlock()

	if err := writeMeta(filepath.Join(chunkTempBase, uploadID), metaSnap); err != nil {
		GoLog.Warnf("chunk upload: failed to persist meta for %q: %v", uploadID, err)
	}
	if done {
		if err := assemble(&metaSnap, uploadID); err != nil {
			return false, err
		}
		cleanupSession(uploadID)
		GoLog.Infof("chunk upload complete: %q → %q", metaSnap.Filename, metaSnap.DestDir)
	}
	return done, nil
}

// assemble writes all chunks sequentially into the destination file.
// Uses a .tmp file + os.Rename for an atomic result.
func assemble(meta *sessionMeta, uploadID string) error {
	dest := resolveDestPath(meta.DestDir, meta.Filename)
	tmp := dest + ".tmp"

	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating target file: %w", err)
	}

	failed := false
	defer func() {
		out.Close()
		if failed {
			os.Remove(tmp)
		}
	}()

	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	for i := range meta.TotalChunks {
		chunkPath := filepath.Join(chunkTempBase, uploadID, fmt.Sprintf("%05d", i))
		in, err := os.Open(chunkPath)
		if err != nil {
			failed = true
			return fmt.Errorf("missing chunk %d: %w", i, err)
		}
		_, err = io.CopyBuffer(out, in, *buf)
		in.Close()
		if err != nil {
			failed = true
			return fmt.Errorf("assembling chunk %d: %w", i, err)
		}
	}

	if err := out.Close(); err != nil {
		failed = true
		return fmt.Errorf("closing tmp file: %w", err)
	}
	return os.Rename(tmp, dest)
}

// resolveDestPath returns dest/filename, appending a nanosecond suffix on collision.
func resolveDestPath(dir, filename string) string {
	dest := filepath.Join(dir, filename)
	if _, err := os.Stat(dest); err != nil {
		return dest
	}
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, time.Now().UnixNano(), ext))
}

func cleanupSession(uploadID string) {
	sessionsMu.Lock()
	delete(sessions, uploadID)
	sessionsMu.Unlock()
	os.RemoveAll(filepath.Join(chunkTempBase, uploadID))
}

// StartReaper periodically removes sessions that have been inactive longer than
// inactivityTimeout. It scans disk instead of the RAM map so it also catches
// sessions left behind by a server restart.
func (s *LocalStorage) StartReaper() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.mu.RLock()
			timeout := s.inactivityTimeout
			s.mu.RUnlock()

			entries, err := os.ReadDir(chunkTempBase)
			if err != nil {
				continue
			}

			now := time.Now()
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				s.reapEntry(e.Name(), now, timeout)
			}
		}
	}()
}

func (s *LocalStorage) reapEntry(id string, now time.Time, timeout time.Duration) {
	dir := filepath.Join(chunkTempBase, id)
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		os.RemoveAll(dir)
		return
	}
	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		os.RemoveAll(dir)
		return
	}
	if now.Sub(meta.LastActivity) <= timeout {
		return
	}
	sessionsMu.Lock()
	delete(sessions, id)
	sessionsMu.Unlock()
	os.RemoveAll(dir)
	GoLog.Infof("chunk upload: reaped inactive session %q (%s)", id, meta.Filename)
}

// writeMeta atomically writes meta to dir/meta.json via a temp file + rename.
func writeMeta(dir string, meta sessionMeta) error {
	path := filepath.Join(dir, "meta.json")
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("writing meta: %w", err)
	}
	if err := json.NewEncoder(f).Encode(meta); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("encoding meta: %w", err)
	}
	f.Close()
	return os.Rename(tmp, path)
}

func missingChunks(received map[int]struct{}, total int) []int {
	missing := make([]int, 0, total-len(received))
	for i := 0; i < total; i++ {
		if _, ok := received[i]; !ok {
			missing = append(missing, i)
		}
	}
	return missing
}

func toSlice(received map[int]struct{}) []int {
	s := make([]int, 0, len(received))
	for i := range received {
		s = append(s, i)
	}
	return s
}

func sanitizeFilename(name string) string {
	return filepath.Base(name)
}
