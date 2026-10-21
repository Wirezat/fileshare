package upload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(format string, args ...any) {
	r.mu.Lock()
	r.events = append(r.events, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}

func (r *recorder) UploadStarted(key string)          { r.add("new %s", key) }
func (r *recorder) ChunkStarted(key string)           { r.add("start %s", key) }
func (r *recorder) ChunkFinished(key string, n int64) { r.add("chunk %s %d", key, n) }
func (r *recorder) UploadPublished(key string)        { r.add("published %s", key) }
func (r *recorder) UploadAbandoned(key string)        { r.add("abandoned %s", key) }

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func observed(t *testing.T) (*Storage, string, *recorder) {
	s, dest := newStorage(t)
	rec := &recorder{}
	s.Observe(rec)
	return s, dest, rec
}

func TestObserverHearsChunksAndPublish(t *testing.T) {
	s, dest, rec := observed(t)
	initSession(t, s, "a.txt", 5, 3, dest)
	send(t, s, 1, "dd")
	send(t, s, 1, "dd")
	send(t, s, 0, "aaa")
	key := s.key(id, dest)
	want := []string{
		"new " + key,
		"start " + key, "chunk " + key + " 2",
		"start " + key, "chunk " + key + " 3", "published " + key,
	}
	if got := rec.got(); !slices.Equal(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}

func TestObserverCountsBytesOfFailedChunks(t *testing.T) {
	s, dest, rec := observed(t)
	initSession(t, s, "a.txt", 6, 3, dest)
	s.Receive(id, dest, 0, strings.NewReader("a"))
	s.Receive(id, dest, 1, strings.NewReader("bbbb"))
	s.Receive(id, dest, 0, &failAfter{data: "aa", err: errors.New("gone")})
	key := s.key(id, dest)
	want := []string{
		"new " + key,
		"start " + key, "chunk " + key + " 1",
		"start " + key, "chunk " + key + " 4",
		"start " + key, "chunk " + key + " 2",
	}
	if got := rec.got(); !slices.Equal(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}

func TestObserverHearsSweptSessions(t *testing.T) {
	s, dest, rec := observed(t)
	initSession(t, s, "a.txt", 2, 1, dest)
	key := s.key(id, dest)
	s.sweep(time.Now())
	if got := rec.got(); len(got) != 1 {
		t.Fatalf("fresh session reported: %v", got)
	}
	s.sweep(time.Now().Add(2 * time.Hour))
	if got, want := rec.got(), []string{"new " + key, "abandoned " + key}; !slices.Equal(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}

func TestObserverHearsReplacedSessions(t *testing.T) {
	s, dest, rec := observed(t)
	initSession(t, s, "a.txt", 4, 2, dest)
	initSession(t, s, "a.txt", 4, 2, dest)
	initSession(t, s, "a.txt", 6, 2, dest)
	key := s.key(id, dest)
	if got, want := rec.got(), []string{"new " + key, "abandoned " + key, "new " + key}; !slices.Equal(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}

func TestObserverHearsSessionsLeftFromAnotherRun(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 4, 2, dest)
	again := New(s.tempBase, func() time.Duration { return time.Hour })
	rec := &recorder{}
	again.Observe(rec)
	if _, err := again.Init(id, "a.txt", 6, 2, dest); err != nil {
		t.Fatal(err)
	}
	key := s.key(id, dest)
	if got, want := rec.got(), []string{"abandoned " + key, "new " + key}; !slices.Equal(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}

func TestObserverHearsSessionsWithBrokenMeta(t *testing.T) {
	s, dest, rec := observed(t)
	initSession(t, s, "a.txt", 2, 1, dest)
	dir := sessionDir(t, s)
	os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{torn"), 0o600)
	orphan := filepath.Join(s.tempBase, "orphan-0001")
	os.MkdirAll(orphan, 0o700)
	s.sweep(time.Now())
	got := rec.got()
	slices.Sort(got)
	key := s.key(id, dest)
	if want := []string{"abandoned orphan-0001", "abandoned " + key, "new " + key}; !slices.Equal(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}

func TestResumingASessionIsNoNewUpload(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 4, 2, dest)
	send(t, s, 0, "aa")
	again := New(s.tempBase, func() time.Duration { return time.Hour })
	rec := &recorder{}
	again.Observe(rec)
	if _, err := again.Init(id, "a.txt", 4, 2, dest); err != nil {
		t.Fatal(err)
	}
	if got := rec.got(); len(got) != 0 {
		t.Errorf("resume reported %v", got)
	}
}
