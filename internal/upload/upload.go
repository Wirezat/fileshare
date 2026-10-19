package upload

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
)

const (
	MaxTotalChunks = 1_000_000
	MaxChunkSize   = 64 << 20
	metaVersion    = 2
	sweepInterval  = 5 * time.Minute
)

var minChunkSize int64 = 64 << 10

var (
	idPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}(-[0-9a-f]{16})?$`)
)

var (
	ErrBadID          = errors.New("upload: invalid uploadId")
	ErrBadSize        = errors.New("upload: invalid file or chunk size")
	ErrChunkLength    = errors.New("upload: chunk has the wrong length")
	ErrUnknownSession = errors.New("upload: unknown upload session")
	ErrBadIndex       = errors.New("upload: chunk index out of range")
)

// ValidID reports whether a client-supplied upload ID may name a session.
func ValidID(id string) bool { return idPattern.MatchString(id) }

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

type sessionMeta struct {
	Version      int       `json:"version"`
	UploadID     string    `json:"uploadId"`
	Filename     string    `json:"filename"`
	DestDir      string    `json:"destDir"`
	Size         int64     `json:"size"`
	ChunkSize    int64     `json:"chunkSize"`
	Part         string    `json:"part"`
	LastActivity time.Time `json:"lastActivity"`
}

func (m *sessionMeta) totalChunks() int { return chunkCount(m.Size, m.ChunkSize) }

func (m *sessionMeta) chunkLength(index int) int64 {
	return min(m.ChunkSize, m.Size-int64(index)*m.ChunkSize)
}

func chunkCount(size, chunkSize int64) int {
	return max(1, int((size+chunkSize-1)/chunkSize))
}

type chunkSession struct {
	meta      sessionMeta
	dir       string
	mu        sync.Mutex
	received  map[int]struct{}
	gone      bool
	published bool
}

// Storage keeps chunked upload sessions: metadata under a temp directory, the data in a hidden file next to its destination.
type Storage struct {
	tempBase string
	timeout  func() time.Duration
	mu       sync.RWMutex
	sessions map[string]*chunkSession
	swept    time.Time
}

// New returns a Storage with session metadata under tempBase; timeout returns how long an idle session survives.
func New(tempBase string, timeout func() time.Duration) *Storage {
	return &Storage{
		tempBase: filepath.Clean(tempBase),
		timeout:  timeout,
		sessions: map[string]*chunkSession{},
	}
}

// key names the session of uploadID for destDir, so the same file uploaded into two shares gets two sessions.
func (s *Storage) key(uploadID, destDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(destDir)))
	return uploadID + "-" + hex.EncodeToString(sum[:8])
}

func (s *Storage) dir(key string) (string, error) {
	if !keyPattern.MatchString(key) {
		return "", ErrBadID
	}
	dir := filepath.Join(s.tempBase, key)
	if !strings.HasPrefix(dir, s.tempBase+string(filepath.Separator)) {
		return "", ErrBadID
	}
	return dir, nil
}

// Init registers or resumes the upload of a file of size bytes sent in chunks of chunkSize into destDir and returns the chunk indices still missing.
func (s *Storage) Init(uploadID, filename string, size, chunkSize int64, destDir string) ([]int, error) {
	if !ValidID(uploadID) {
		return nil, ErrBadID
	}
	if size < 0 || chunkSize < 1 || chunkSize > MaxChunkSize || (size+chunkSize-1)/chunkSize > MaxTotalChunks ||
		(chunkSize < minChunkSize && size > chunkSize) {
		return nil, ErrBadSize
	}
	s.sweepIfDue(time.Now())
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.loadOrCreate(uploadID, filename, size, chunkSize, filepath.Clean(destDir))
	if err != nil {
		return nil, err
	}

	sess.mu.Lock()
	missing := missingChunks(sess.received, sess.meta.totalChunks())
	sess.meta.LastActivity = time.Now()
	if err := writeMeta(sess.dir, sess.meta); err != nil {
		GoLog.Warnf("chunk upload: failed to persist meta for %q: %v (non-fatal)", uploadID, err)
	}
	sess.mu.Unlock()

	GoLog.Debugf("chunk upload init/resume: id=%q missing=%d", uploadID, len(missing))
	return missing, nil
}

func (s *Storage) loadOrCreate(uploadID, filename string, size, chunkSize int64, destDir string) (*chunkSession, error) {
	key := s.key(uploadID, destDir)
	matches := func(m sessionMeta) bool {
		return m.Version == metaVersion && m.Size == size && m.ChunkSize == chunkSize && m.DestDir == destDir
	}
	if sess, ok := s.sessions[key]; ok {
		if matches(sess.meta) {
			return sess, nil
		}
		delete(s.sessions, key)
		sess.mu.Lock()
		sess.gone = true
		sess.mu.Unlock()
		discard(sess.meta)
	}

	dir, err := s.dir(key)
	if err != nil {
		return nil, err
	}

	if data, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
		var meta sessionMeta
		if json.Unmarshal(data, &meta) == nil && matches(meta) {
			if _, err := os.Stat(meta.Part); err == nil {
				sess := &chunkSession{meta: meta, dir: dir, received: readReceived(dir, meta.totalChunks())}
				s.sessions[key] = sess
				return sess, nil
			}
		}
		discard(meta)
		GoLog.Infof("chunk upload: restarting session %q in the current format", uploadID)
	}
	os.RemoveAll(dir)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating session dir for %q: %w", uploadID, err)
	}
	part, err := createPart(destDir, size)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	meta := sessionMeta{
		Version:      metaVersion,
		UploadID:     uploadID,
		Filename:     sanitizeFilename(filename),
		DestDir:      destDir,
		Size:         size,
		ChunkSize:    chunkSize,
		Part:         part,
		LastActivity: time.Now(),
	}
	if err := writeMeta(dir, meta); err != nil {
		os.Remove(part)
		os.RemoveAll(dir)
		return nil, err
	}
	sess := &chunkSession{meta: meta, dir: dir, received: make(map[int]struct{})}
	s.sessions[key] = sess
	return sess, nil
}

// readReceived returns the chunk indices listed in dir/received, ignoring a torn last entry and indices out of range.
func readReceived(dir string, total int) map[int]struct{} {
	received := map[int]struct{}{}
	data, _ := os.ReadFile(filepath.Join(dir, "received"))
	for off := 0; off+4 <= len(data); off += 4 {
		if i := int(binary.LittleEndian.Uint32(data[off:])); i < total {
			received[i] = struct{}{}
		}
	}
	return received
}

func appendReceived(dir string, index int) error {
	f, err := os.OpenFile(filepath.Join(dir, "received"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(binary.LittleEndian.AppendUint32(nil, uint32(index))); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// createPart creates a hidden, randomly named file of size bytes in destDir and returns its path.
func createPart(destDir string, size int64) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	path := filepath.Join(destDir, ".fileshare-upload-"+hex.EncodeToString(b[:])+".part")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("creating partial file: %w", err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("sizing partial file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("closing partial file: %w", err)
	}
	return path, nil
}

func discard(meta sessionMeta) {
	if meta.Part != "" && filepath.Dir(meta.Part) == filepath.Clean(meta.DestDir) && strings.HasPrefix(filepath.Base(meta.Part), ".fileshare-upload-") {
		os.Remove(meta.Part)
	}
}

// Receive writes chunk index, read from r, at its place in the file uploaded into destDir and reports whether the file is complete; r must hold exactly the chunk's length.
func (s *Storage) Receive(uploadID, destDir string, index int, r io.Reader) (bool, error) {
	s.mu.RLock()
	sess, ok := s.sessions[s.key(uploadID, filepath.Clean(destDir))]
	s.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrUnknownSession, uploadID)
	}

	sess.mu.Lock()
	meta := sess.meta
	total := meta.totalChunks()
	_, alreadyReceived := sess.received[index]
	complete := len(sess.received) == total
	sess.mu.Unlock()
	if index < 0 || index >= total {
		return false, fmt.Errorf("%w: %d not in [0, %d)", ErrBadIndex, index, total)
	}
	if alreadyReceived {
		return complete, nil
	}

	writeErr := writeChunk(meta.Part, int64(index)*meta.ChunkSize, meta.chunkLength(index), r)

	sess.mu.Lock()
	if sess.gone {
		published := sess.published
		sess.mu.Unlock()
		if published {
			return true, nil
		}
		return false, fmt.Errorf("%w: %q", ErrUnknownSession, uploadID)
	}
	if writeErr != nil {
		sess.mu.Unlock()
		return false, fmt.Errorf("writing chunk %d: %w", index, writeErr)
	}
	sess.received[index] = struct{}{}
	sess.meta.LastActivity = time.Now()
	if err := appendReceived(sess.dir, index); err != nil {
		GoLog.Warnf("chunk upload: failed to record chunk %d of %q: %v (non-fatal)", index, uploadID, err)
	}
	done := len(sess.received) == total
	if !done {
		sess.mu.Unlock()
		return false, nil
	}
	dest, err := publish(meta.Part, meta.DestDir, meta.Filename)
	if err != nil {
		sess.mu.Unlock()
		return false, err
	}
	sess.gone, sess.published = true, true
	sess.mu.Unlock()
	s.cleanup(sess)
	GoLog.Infof("chunk upload complete: %q → %q", meta.Filename, dest)
	return true, nil
}

func writeChunk(part string, offset, length int64, r io.Reader) error {
	f, err := os.OpenFile(part, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	buf := bufPool.Get().(*[]byte)
	n, err := io.CopyBuffer(io.NewOffsetWriter(f, offset), io.LimitReader(r, length), *buf)
	bufPool.Put(buf)
	if err != nil {
		f.Close()
		return err
	}
	if n != length {
		f.Close()
		return fmt.Errorf("%w: got %d bytes, want %d", ErrChunkLength, n, length)
	}
	var extra [1]byte
	m, err := r.Read(extra[:])
	if m > 0 {
		f.Close()
		return fmt.Errorf("%w: more than %d bytes", ErrChunkLength, length)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		f.Close()
		return err
	}
	return f.Close()
}

// publish gives the finished partial file its name in dir without replacing an existing file and returns the final path.
func publish(part, dir, filename string) (string, error) {
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	for attempt := range 100 {
		dest := filepath.Join(dir, filename)
		if attempt > 0 {
			dest = filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, time.Now().UnixNano(), ext))
		}
		err := os.Link(part, dest)
		if err == nil {
			os.Remove(part)
			return dest, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if _, statErr := os.Lstat(dest); errors.Is(statErr, fs.ErrNotExist) {
			if err := os.Rename(part, dest); err != nil {
				return "", fmt.Errorf("publishing upload: %w", err)
			}
			return dest, nil
		}
	}
	return "", fmt.Errorf("publishing upload: no free name for %q", filename)
}

func (s *Storage) cleanup(sess *chunkSession) {
	s.mu.Lock()
	key := filepath.Base(sess.dir)
	if s.sessions[key] == sess {
		delete(s.sessions, key)
	}
	s.mu.Unlock()
	os.RemoveAll(sess.dir)
}

// Sweep removes sessions idle longer than the timeout, with their partial files, and directories without meta.json.
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
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		delete(s.sessions, id)
		os.RemoveAll(dir)
		return
	}
	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		delete(s.sessions, id)
		os.RemoveAll(dir)
		return
	}
	if fi, err := os.Stat(filepath.Join(dir, "received")); err == nil && fi.ModTime().After(meta.LastActivity) {
		meta.LastActivity = fi.ModTime()
	}
	sess, loaded := s.sessions[id]
	if loaded {
		sess.mu.Lock()
		meta.LastActivity = sess.meta.LastActivity
		sess.mu.Unlock()
	}
	if now.Sub(meta.LastActivity) <= timeout {
		return
	}
	if loaded {
		sess.mu.Lock()
		sess.gone = true
		sess.mu.Unlock()
	}
	delete(s.sessions, id)
	discard(meta)
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
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing meta: %w", err)
	}
	return os.Rename(tmp, path)
}

func missingChunks(received map[int]struct{}, total int) []int {
	missing := make([]int, 0, total-len(received))
	for i := range total {
		if _, ok := received[i]; !ok {
			missing = append(missing, i)
		}
	}
	return slices.Clip(missing)
}

func sanitizeFilename(name string) string {
	return filepath.Base(name)
}
