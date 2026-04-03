package shared

import (
	"bufio"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	maxLogEntries = 500
	tailInterval  = 200 * time.Millisecond
)

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

// ParseLine parses a log line of the form: [LEVEL] [timestamp] message
func ParseLine(line string) (LogEntry, bool) {
	line = strings.TrimSpace(line)
	if len(line) == 0 || line[0] != '[' {
		return LogEntry{}, false
	}
	i := strings.IndexByte(line, ']')
	if i < 0 {
		return LogEntry{}, false
	}
	level, rest := line[1:i], strings.TrimSpace(line[i+1:])

	if len(rest) == 0 || rest[0] != '[' {
		return LogEntry{}, false
	}
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return LogEntry{}, false
	}
	msg := ""
	if j+2 <= len(rest) {
		msg = rest[j+2:]
	}
	return LogEntry{Level: level, Time: rest[1:j], Message: msg}, true
}

func (l *LogStore) add(entry LogEntry) {
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
		}
	}
}

// Recent returns up to n of the most recent log entries.
func (l *LogStore) Recent(n int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	total := len(l.entries)
	if n <= 0 || total == 0 {
		return nil
	}
	if n > total {
		n = total
	}
	out := make([]LogEntry, n)
	copy(out, l.entries[total-n:])
	return out
}

// Subscribe returns a channel that receives all future log entries.
func (l *LogStore) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 64)
	l.mu.Lock()
	l.subs = append(l.subs, ch)
	l.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
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

// Load reads a log file and populates the ring buffer.
func (l *LogStore) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if entry, ok := ParseLine(s.Text()); ok {
			l.add(entry)
		}
	}
	return s.Err()
}

// Tail watches a log file for new lines and feeds them into the ring buffer.
// It seeks to the end of the file first so already-loaded entries are not duplicated.
func (l *LogStore) Tail(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return err
	}
	go func() {
		defer f.Close()
		r := bufio.NewReader(f)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				time.Sleep(tailInterval)
				continue
			}
			if entry, ok := ParseLine(strings.TrimRight(line, "\n")); ok {
				l.add(entry)
			}
		}
	}()
	return nil
}
