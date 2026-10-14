package logstore

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestRecentReturnsNewestInOrder(t *testing.T) {
	s := New()
	for i := range 3 {
		s.Add(Entry{Message: strconv.Itoa(i)})
	}
	got := s.Recent(2)
	if len(got) != 2 || got[0].Message != "1" || got[1].Message != "2" {
		t.Errorf("Recent(2) = %+v", got)
	}
	if len(s.Recent(10)) != 3 {
		t.Error("Recent(10) did not return all three")
	}
	if s.Recent(0) != nil || New().Recent(5) != nil {
		t.Error("Recent on n=0 or empty store not nil")
	}
}

func TestKeepsOnlyTheLastMaxEntries(t *testing.T) {
	s := New()
	for i := range maxEntries + 10 {
		s.Add(Entry{Message: strconv.Itoa(i)})
	}
	got := s.Recent(maxEntries + 10)
	if len(got) != maxEntries || got[0].Message != "10" {
		t.Errorf("len %d, first %q", len(got), got[0].Message)
	}
}

func TestSubscribeReceivesUntilUnsubscribed(t *testing.T) {
	s := New()
	ch := s.Subscribe()
	s.Add(Entry{Message: "hi"})
	if e := <-ch; e.Message != "hi" {
		t.Errorf("got %q", e.Message)
	}
	s.Unsubscribe(ch)
	if _, ok := <-ch; ok {
		t.Error("channel still open")
	}
	s.Add(Entry{Message: "after"})
}

func TestSlowSubscriberDoesNotBlockAdd(t *testing.T) {
	s := New()
	s.Subscribe()
	done := make(chan struct{})
	go func() {
		for range 1000 {
			s.Add(Entry{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Add blocked on a full subscriber")
	}
}

func TestLoadReadsTextAndJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	content := "[INFO] [2026-10-14 10:00:00] server started\n" +
		`{"level":"INFO","time":"t1","message":"GET /docs","request":{"method":"GET"}}` + "\n" +
		"garbage\n" +
		"[WARN] [t2]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New()
	if err := s.Load(path); err != nil {
		t.Fatal(err)
	}
	got := s.Recent(10)
	if len(got) != 3 {
		t.Fatalf("loaded %d entries, want 3: %+v", len(got), got)
	}
	if e := got[0]; e.Level != "INFO" || e.Time != "2026-10-14 10:00:00" || e.Message != "server started" || e.Request != nil {
		t.Errorf("text line = %+v", e)
	}
	if got[1].Message != "GET /docs" || string(got[1].Request) != `{"method":"GET"}` {
		t.Errorf("json line = %+v", got[1])
	}
	if got[2].Level != "WARN" || got[2].Time != "t2" || got[2].Message != "" {
		t.Errorf("empty-message line = %+v", got[2])
	}
}

func TestLoadMissingFileFails(t *testing.T) {
	if err := New().Load(filepath.Join(t.TempDir(), "nope.log")); err == nil {
		t.Error("Load of missing file returned nil")
	}
}
