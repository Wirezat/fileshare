package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/logstore"
)

var logStore = logstore.New()

const (
	maxBodyBytes = 1 << 20 // 1 MB
	logmaxLen    = 4096
)

// requestLog is the structured log entry for an HTTP request.
type requestLog struct {
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	ClientIP   string            `json:"client_ip"`
	Headers    map[string]string `json:"headers"`
	Body       any               `json:"body,omitempty"`
	BodyError  string            `json:"body_error,omitempty"`
	FormFields map[string]string `json:"form_fields,omitempty"`
}

// loggingMiddleware records every request in the admin log store.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/log" {
			next.ServeHTTP(w, r)
			return
		}

		rl := buildRequestLog(r)
		if data, err := rl.toJSON(); err == nil {
			entry := logstore.Entry{
				Level:   "INFO",
				Time:    time.Now().Format(time.RFC3339),
				Message: fmt.Sprintf("%s %s", rl.Method, rl.URL),
				Request: data,
			}
			logStore.Add(entry)
			persistRequestLog(entry)
		} else {
			GoLog.Warnf("loggingMiddleware: failed to serialize request: %v", err)
		}

		next.ServeHTTP(w, r)
	})
}

var (
	requestLogMu   sync.Mutex
	requestLogFile *os.File
)

// openRequestLog opens GoLog's log file for appending request-log entries, one JSON object per line.
func openRequestLog() {
	path := GoLog.LogPath()
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		GoLog.Warnf("openRequestLog: %v", err)
		return
	}
	requestLogMu.Lock()
	requestLogFile = f
	requestLogMu.Unlock()
}

func persistRequestLog(entry logstore.Entry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	requestLogMu.Lock()
	defer requestLogMu.Unlock()
	if requestLogFile != nil {
		fmt.Fprintln(requestLogFile, string(data))
	}
}

func (rl *requestLog) toJSON() ([]byte, error) {
	return json.Marshal(rl)
}

// buildRequestLog constructs a requestLog from the incoming request.
func buildRequestLog(r *http.Request) *requestLog {
	decodedURL, err := url.QueryUnescape(r.URL.RequestURI())
	if err != nil {
		decodedURL = r.URL.RequestURI()
	}

	entry := &requestLog{
		Method:   r.Method,
		URL:      decodedURL,
		ClientIP: clientIP(r),
		Headers:  safeHeaders(r),
	}

	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		return entry
	}

	ct := r.Header.Get("Content-Type")
	switch {

	case strings.HasPrefix(ct, "application/json"):
		body, err := readAndRestoreBody(r)
		if err != nil {
			entry.BodyError = err.Error()
			break
		}
		var parsed any
		if json.Unmarshal(body, &parsed) == nil {
			entry.Body = parsed
		} else {
			entry.Body = string(body)
		}

	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		body, err := readAndRestoreBody(r)
		if err != nil {
			entry.BodyError = err.Error()
			break
		}
		if form, err := url.ParseQuery(string(body)); err == nil {
			entry.FormFields = flattenValues(form)
		}
	}

	return entry
}

// Headers excluded from request logs.
var sensitiveHeaders = map[string]bool{
	http.CanonicalHeaderKey("authorization"): true,
	http.CanonicalHeaderKey("cookie"):        true,
	http.CanonicalHeaderKey("x-auth-token"):  true,
}

// safeHeaders returns request headers with sensitive fields stripped.
func safeHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string, len(r.Header))
	for name, values := range r.Header {
		if sensitiveHeaders[name] || len(values) == 0 {
			continue
		}
		headers[name] = values[0]
	}
	return headers
}

// readAndRestoreBody reads up to maxBodyBytes and resets r.Body for downstream handlers.
func readAndRestoreBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, err
}

// flattenValues collapses a string-slice map to a string map (first value wins).
func flattenValues(m map[string][]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

func preventClientLogInjection(input string) string {
	if len(input) > logmaxLen {
		input = input[:logmaxLen]
	}
	var builder strings.Builder
	builder.Grow(len(input))
	insideAnsiEscape := false
	for offset, char := range input {
		if char == 0x1b && offset+1 < len(input) && input[offset+1] == '[' {
			insideAnsiEscape = true
			continue
		}
		if insideAnsiEscape {
			if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' {
				insideAnsiEscape = false
			}
			continue
		}
		if char >= 32 && char != 127 {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}
