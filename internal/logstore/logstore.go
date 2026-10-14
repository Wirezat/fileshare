package logstore

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
)

const maxEntries = 500

// Entry is one line of the admin log view; Request is set only for HTTP access log entries.
type Entry struct {
	Level   string          `json:"level"`
	Time    string          `json:"time"`
	Message string          `json:"message"`
	Request json.RawMessage `json:"request,omitempty"`
}

// Store is a ring buffer of the most recent entries that pushes new ones to subscribers.
type Store struct {
	mu      sync.RWMutex
	entries []Entry
	subs    []chan Entry
}

func New() *Store {
	return &Store{entries: make([]Entry, 0, maxEntries)}
}

// parseLine parses a log line of the form: [LEVEL] [timestamp] message
func parseLine(line string) (Entry, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || line[0] != '[' {
		return Entry{}, false
	}
	i := strings.IndexByte(line, ']')
	if i < 0 {
		return Entry{}, false
	}
	level, rest := line[1:i], strings.TrimSpace(line[i+1:])
	if len(rest) < 2 || rest[0] != '[' {
		return Entry{}, false
	}
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return Entry{}, false
	}
	msg := ""
	if j+2 < len(rest) {
		msg = rest[j+2:]
	}
	return Entry{Level: level, Time: rest[1:j], Message: msg}, true
}

// Add appends entry, dropping the oldest beyond maxEntries, and offers it to every subscriber without blocking.
func (s *Store) Add(entry Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= maxEntries {
		copy(s.entries, s.entries[1:])
		s.entries = s.entries[:len(s.entries)-1]
	}
	s.entries = append(s.entries, entry)
	for _, ch := range s.subs {
		select {
		case ch <- entry:
		default:
		}
	}
}

// Recent returns up to n of the newest entries, oldest first, or nil if there are none.
func (s *Store) Recent(n int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.entries)
	if n <= 0 || total == 0 {
		return nil
	}
	n = min(n, total)
	out := make([]Entry, n)
	copy(out, s.entries[total-n:])
	return out
}

func (s *Store) Subscribe() chan Entry {
	ch := make(chan Entry, 64)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (s *Store) Unsubscribe(ch chan Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subs {
		if sub == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Load adds every GoLog text line and JSON-encoded Entry line of the file at path.
func (s *Store) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "{") {
			var entry Entry
			if json.Unmarshal([]byte(line), &entry) == nil {
				s.Add(entry)
			}
			continue
		}
		if entry, ok := parseLine(line); ok {
			s.Add(entry)
		}
	}
	return sc.Err()
}
