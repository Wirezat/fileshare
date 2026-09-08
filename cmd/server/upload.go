package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

const chunkTempBase = "/tmp/fileshare-chunks"

// maxTotalChunks bounds the loops that walk a session's chunk list. The value
// comes from the client, and without a cap a single request could ask the
// server to iterate billions of times.
const maxTotalChunks = 1_000_000

// uploadIDPattern is a strict allowlist, not a denylist of dangerous
// characters. The ID names a directory under chunkTempBase and is supplied by
// whoever is uploading; with anything path-like allowed through, filepath.Join
// resolves it and the session's own os.MkdirAll, os.Create and — worst —
// os.RemoveAll act wherever the caller pointed them. The browser client sends
// a 32-character hex digest, so this costs nothing legitimate.
var uploadIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

var errBadUploadID = errors.New("upload: invalid uploadId")

// validUploadID reports whether an ID may be used to build a path.
func validUploadID(id string) bool { return uploadIDPattern.MatchString(id) }

// sessionDir maps an upload ID to its temp directory, refusing anything that
// could escape chunkTempBase. Every path in this file goes through here rather
// than joining chunkTempBase directly, so a future caller cannot reintroduce
// the hole by forgetting the check.
func sessionDir(uploadID string) (string, error) {
	if !validUploadID(uploadID) {
		return "", errBadUploadID
	}
	dir := filepath.Join(chunkTempBase, uploadID)
	// Belt and braces: the pattern already forbids separators and dots, so this
	// can only fire if the pattern is ever loosened.
	if dir != chunkTempBase && !strings.HasPrefix(dir, chunkTempBase+"/") {
		return "", errBadUploadID
	}
	return dir, nil
}

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
// NOTE: Received[] is intentionally NOT persisted here anymore.
// The filesystem (presence of chunk files) is the source of truth.
// meta.json only stores the invariant session metadata.
type sessionMeta struct {
	UploadID     string    `json:"uploadId"`
	Filename     string    `json:"filename"`
	TotalChunks  int       `json:"totalChunks"`
	DestDir      string    `json:"destDir"`
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

// loadOrCreateSession checks for an existing upload session in RAM or on disk,
// or creates a new one if not found.
// When resuming from disk, it scans actual chunk files to rebuild received state —
// not meta.json — so a crash between writing the chunk and updating meta.json
// never causes a chunk to be re-sent unnecessarily.
func (s *LocalStorage) loadOrCreateSession(uploadID, filename string, totalChunks int, destDir string) (*chunkSession, error) {
	// 1. RAM hit
	if sess, ok := sessions[uploadID]; ok {
		return sess, nil
	}

	dir, err := sessionDir(uploadID)
	if err != nil {
		return nil, err
	}

	// 2. Disk hit — meta.json exists, rebuild received from actual chunk files
	if data, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("corrupt meta.json for %q: %w", uploadID, err)
		}

		// Scan disk for chunk files — because the server could crash before updating meta.json after a chunk upload.
		// A chunk is considered received if and only if its file exists on disk.
		received, err := scanReceivedChunks(dir, meta.TotalChunks)
		if err != nil {
			return nil, fmt.Errorf("scanning chunks for %q: %w", uploadID, err)
		}

		sess := &chunkSession{meta: meta, received: received}
		sessions[uploadID] = sess
		return sess, nil
	}

	// 3. New session
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating temp dir for %q: %w", uploadID, err)
	}
	meta := sessionMeta{
		UploadID:     uploadID,
		Filename:     sanitizeFilename(filename),
		TotalChunks:  totalChunks,
		DestDir:      destDir,
		LastActivity: time.Now(),
	}
	if err := writeMeta(dir, meta); err != nil {
		return nil, err
	}
	sess := &chunkSession{meta: meta, received: make(map[int]struct{})}
	sessions[uploadID] = sess
	return sess, nil
}

// scanReceivedChunks returns the set of chunk indices whose files actually
// exist on disk. It does NOT rely on meta.json's Received[] field.
func scanReceivedChunks(dir string, totalChunks int) (map[int]struct{}, error) {
	received := make(map[int]struct{}, totalChunks)
	for i := range totalChunks {
		chunkPath := filepath.Join(dir, fmt.Sprintf("%05d", i))
		if _, err := os.Stat(chunkPath); err == nil {
			received[i] = struct{}{}
		}
	}
	return received, nil
}

// ReceiveChunk stores a single chunk and updates meta.json (LastActivity only).
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

	sess.mu.Lock()
	_, alreadyReceived := sess.received[index]
	sess.mu.Unlock()
	if alreadyReceived {
		return len(sess.received) == sess.meta.TotalChunks, nil
	}

	dir, err := sessionDir(uploadID)
	if err != nil {
		return false, err
	}
	chunkPath := filepath.Join(dir, fmt.Sprintf("%05d", index))
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

	// Chunk is now safely on disk. Update in-RAM state and persist LastActivity.
	// meta.json no longer tracks Received[] — chunk files are the source of truth.
	// A failure to write meta.json here is non-fatal: the chunk file exists on disk
	// and will be discovered by scanReceivedChunks on next resume.
	sess.mu.Lock()
	sess.received[index] = struct{}{}
	sess.meta.LastActivity = time.Now()
	metaSnap := sess.meta
	done := len(sess.received) == sess.meta.TotalChunks
	sess.mu.Unlock()

	if err := writeMeta(dir, metaSnap); err != nil {
		GoLog.Warnf("chunk upload: failed to persist meta for %q: %v (non-fatal)", uploadID, err)
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
	dir, err := sessionDir(uploadID)
	if err != nil {
		return err
	}
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
		chunkPath := filepath.Join(dir, fmt.Sprintf("%05d", i))
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
	if err := os.Rename(tmp, dest); err != nil {
		failed = true
		return fmt.Errorf("renaming assembled file: %w", err)
	}
	return nil
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
	dir, err := sessionDir(uploadID)
	if err != nil {
		return
	}
	os.RemoveAll(dir)
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
	dir, err := sessionDir(id)
	if err != nil {
		return
	}
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
// meta.json stores only invariant session data (no Received[] list).
// The received set is derived from actual chunk files on disk — see scanReceivedChunks.
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

func sanitizeFilename(name string) string {
	return filepath.Base(name)
}
