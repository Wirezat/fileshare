package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/logstore"
)

const logmaxLen = 4096

// requestLog is the structured log entry for an HTTP request.
type requestLog struct {
	Method    string `json:"method"`
	URL       string `json:"url"`
	ClientIP  string `json:"client_ip"`
	UserAgent string `json:"user_agent,omitempty"`
}

// loggingMiddleware records every request in the admin log store.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
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
			s.deps.Logs.Add(entry)
			s.persistRequestLog(entry)
		} else {
			GoLog.Warnf("loggingMiddleware: failed to serialize request: %v", err)
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) persistRequestLog(entry logstore.Entry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	s.requestLogMu.Lock()
	defer s.requestLogMu.Unlock()
	if s.deps.RequestLog != nil {
		fmt.Fprintln(s.deps.RequestLog, string(data))
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

	return &requestLog{
		Method:    r.Method,
		URL:       decodedURL,
		ClientIP:  GoLog.IP(clientIP(r)).String(),
		UserAgent: r.UserAgent(),
	}
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
