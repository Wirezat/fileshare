package upload

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
)

// MaxTotalChunks caps the client-supplied chunk count a session may declare.
const MaxTotalChunks = 1_000_000

const sweepInterval = 5 * time.Minute

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

var ErrBadID = errors.New("upload: invalid uploadId")

// ValidID reports whether a client-supplied upload ID may name a session directory.
func ValidID(id string) bool { return idPattern.MatchString(id) }

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

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

// Storage keeps chunked upload sessions under a temp directory and assembles finished files in their destination.
type Storage struct {
	tempBase string
	timeout  func() time.Duration
	mu       sync.RWMutex
	sessions map[string]*chunkSession
	swept    time.Time
}

// New returns a Storage with chunks under tempBase; timeout returns how long an idle session survives.
func New(tempBase string, timeout func() time.Duration) *Storage {
	return &Storage{
		tempBase: filepath.Clean(tempBase),
		timeout:  timeout,
		sessions: map[string]*chunkSession{},
	}
}

func (s *Storage) dir(uploadID string) (string, error) {
	if !ValidID(uploadID) {
		return "", ErrBadID
	}
	dir := filepath.Join(s.tempBase, uploadID)
	if !strings.HasPrefix(dir, s.tempBase+string(filepath.Separator)) {
		return "", ErrBadID
	}
	return dir, nil
}

// Init registers or resumes a session and returns the chunk indices still missing.
func (s *Storage) Init(uploadID, filename string, totalChunks int, destDir string) ([]int, error) {
	s.sweepIfDue(time.Now())
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.loadOrCreate(uploadID, filename, totalChunks, destDir)
	if err != nil {
		return nil, err
	}

	sess.mu.Lock()
	missing := missingChunks(sess.received, sess.meta.TotalChunks)
	sess.mu.Unlock()

	GoLog.Debugf("chunk upload init/resume: id=%q missing=%d", uploadID, len(missing))
	return missing, nil
}

func (s *Storage) loadOrCreate(uploadID, filename string, totalChunks int, destDir string) (*chunkSession, error) {
	if sess, ok := s.sessions[uploadID]; ok {
		return sess, nil
	}

	dir, err := s.dir(uploadID)
	if err != nil {
		return nil, err
	}

	if data, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("corrupt meta.json for %q: %w", uploadID, err)
		}
		received, err := scanReceivedChunks(dir, meta.TotalChunks)
		if err != nil {
			return nil, fmt.Errorf("scanning chunks for %q: %w", uploadID, err)
		}
		sess := &chunkSession{meta: meta, received: received}
		s.sessions[uploadID] = sess
		return sess, nil
	}

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
	s.sessions[uploadID] = sess
	return sess, nil
}

func scanReceivedChunks(dir string, totalChunks int) (map[int]struct{}, error) {
	received := make(map[int]struct{}, totalChunks)
	for i := range totalChunks {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%05d", i))); err == nil {
			received[i] = struct{}{}
		}
	}
	return received, nil
}

// Receive stores one chunk and returns true once every chunk has arrived and the file is assembled.
func (s *Storage) Receive(uploadID string, index int, r io.Reader) (bool, error) {
	s.mu.RLock()
	sess, ok := s.sessions[uploadID]
	s.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("unknown upload session %q", uploadID)
	}
	if index < 0 || index >= sess.meta.TotalChunks {
		return false, fmt.Errorf("chunk index %d out of range [0, %d)", index, sess.meta.TotalChunks)
	}

	sess.mu.Lock()
	_, alreadyReceived := sess.received[index]
	complete := len(sess.received) == sess.meta.TotalChunks
	sess.mu.Unlock()
	if alreadyReceived {
		return complete, nil
	}

	dir, err := s.dir(uploadID)
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
		if err := assemble(&metaSnap, dir); err != nil {
			return false, err
		}
		s.cleanup(uploadID)
		GoLog.Infof("chunk upload complete: %q → %q", metaSnap.Filename, metaSnap.DestDir)
	}
	return done, nil
}

// assemble writes all chunks in dir into the destination file atomically via a .tmp file.
func assemble(meta *sessionMeta, dir string) error {
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
		in, err := os.Open(filepath.Join(dir, fmt.Sprintf("%05d", i)))
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

// resolveDestPath returns dir/filename, appending a nanosecond suffix on collision.
func resolveDestPath(dir, filename string) string {
	dest := filepath.Join(dir, filename)
	if _, err := os.Stat(dest); err != nil {
		return dest
	}
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, time.Now().UnixNano(), ext))
}

func (s *Storage) cleanup(uploadID string) {
	s.mu.Lock()
	delete(s.sessions, uploadID)
	s.mu.Unlock()
	if dir, err := s.dir(uploadID); err == nil {
		os.RemoveAll(dir)
	}
}

// Sweep removes sessions idle longer than the timeout and directories without meta.json.
func (s *Storage) Sweep() {
	now := time.Now()
	s.mu.Lock()
	s.swept = now
	s.mu.Unlock()
	s.sweep(now)
}

func (s *Storage) sweepIfDue(now time.Time) {
	s.mu.Lock()
	due := now.Sub(s.swept) > sweepInterval
	if due {
		s.swept = now
	}
	s.mu.Unlock()
	if due {
		s.sweep(now)
	}
}

func (s *Storage) sweep(now time.Time) {
	entries, err := os.ReadDir(s.tempBase)
	if err != nil {
		return
	}
	timeout := s.timeout()
	for _, e := range entries {
		if e.IsDir() {
			s.sweepEntry(e.Name(), now, timeout)
		}
	}
}

func (s *Storage) sweepEntry(id string, now time.Time, timeout time.Duration) {
	dir, err := s.dir(id)
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
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	os.RemoveAll(dir)
	GoLog.Infof("chunk upload: removed inactive session %q (%s)", id, meta.Filename)
}

// writeMeta atomically writes meta to dir/meta.json via a temp file and rename.
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
