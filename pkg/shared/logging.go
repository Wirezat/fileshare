package shared

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

const maxLogEntries = 500

// ── Types ─────────────────────────────────────────────

type LogEntry struct {
	Level   string `json:"level"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

type LogStore struct {
	mu      sync.RWMutex
	entries []LogEntry
	subs    []chan LogEntry
}

var Logger = &LogStore{}

// ── Parsing ───────────────────────────────────────────

// ParseLine parses a single log line in the format:
//
//	LEVEL [timestamp] message
//
// Returns the parsed LogEntry and true on success, or false if the line
// is empty, a comment, or does not match the expected format.
func ParseLine(line string) (LogEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '[' {
		return LogEntry{}, false
	}

	close1 := strings.IndexByte(line, ']')
	if close1 < 0 {
		return LogEntry{}, false
	}
	level := line[1:close1]

	rest := strings.TrimSpace(line[close1+1:])
	if len(rest) == 0 || rest[0] != '[' {
		return LogEntry{}, false
	}
	close2 := strings.IndexByte(rest, ']')
	if close2 < 0 {
		return LogEntry{}, false
	}
	timestamp := rest[1:close2]

	message := ""
	if close2+2 <= len(rest) {
		message = rest[close2+2:]
	}

	return LogEntry{
		Level:   level,
		Time:    timestamp,
		Message: message,
	}, true
}

// ── Ring buffer ───────────────────────────────────────

// Add appends a LogEntry to the ring buffer and broadcasts it to all
// active subscribers. If the buffer exceeds maxLogEntries, the oldest
// entry is dropped.
func (l *LogStore) Add(entry LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) >= maxLogEntries {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, entry)

	for _, ch := range l.subs {
		select {
		case ch <- entry:
		default:
			// Subscriber too slow — drop rather than block
		}
	}
}

// Recent returns up to n of the most recent log entries.
func (l *LogStore) Recent(n int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if n <= 0 || len(l.entries) == 0 {
		return nil
	}
	if n >= len(l.entries) {
		result := make([]LogEntry, len(l.entries))
		copy(result, l.entries)
		return result
	}
	start := len(l.entries) - n
	result := make([]LogEntry, n)
	copy(result, l.entries[start:])
	return result
}

// ── Pub / Sub ─────────────────────────────────────────

// Subscribe registers a new subscriber and returns a channel that
// receives every future LogEntry added via Add.
// The channel is buffered to avoid blocking the broadcaster.
func (l *LogStore) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 64)
	l.mu.Lock()
	l.subs = append(l.subs, ch)
	l.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (l *LogStore) Unsubscribe(ch chan LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i, sub := range l.subs {
		if sub == ch {
			l.subs = append(l.subs[:i], l.subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// ── File loading ──────────────────────────────────────

// LoadFromFile reads a log file line by line, parses each line via
// ParseLine, and loads the results into the ring buffer.
// Malformed or empty lines are silently skipped.
func (l *LogStore) LoadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		entry, ok := ParseLine(scanner.Text())
		if !ok {
			continue
		}
		l.Add(entry)
	}
	return scanner.Err()
}
