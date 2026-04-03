package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	maxBodyBytes     = 1 << 20  // 1 MB
	maxMultipartSize = 32 << 20 // 32 MB
)

// Headers excluded from request logs.
var sensitiveHeaders = map[string]bool{
	http.CanonicalHeaderKey("authorization"): true,
	http.CanonicalHeaderKey("cookie"):        true,
	http.CanonicalHeaderKey("x-auth-token"):  true,
}

// requestLog is the structured log entry for an HTTP request.
// omitempty keeps the JSON output lean for non-POST/PUT requests.
type requestLog struct {
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	ClientIP      string            `json:"client_ip"`
	Headers       map[string]string `json:"headers"`
	Body          any               `json:"body,omitempty"`
	BodyError     string            `json:"body_error,omitempty"`
	FormFields    map[string]string `json:"form_fields,omitempty"`
	UploadedFiles []fileInfo        `json:"uploaded_files,omitempty"`
}

type fileInfo struct {
	Field       string `json:"field"`
	Filename    string `json:"filename"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type"`
}

// clientIP extracts the real client IP.
// Priority: X-Forwarded-For (first entry) → CF-Connecting-IP → RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Avoid SplitN allocation — just find the first comma.
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if cf := r.Header.Get("Cf-Connecting-Ip"); cf != "" {
		return strings.TrimSpace(cf)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
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

// requestToJSON serializes an HTTP request to JSON.
// POST/PUT bodies are parsed by content type; the body is always restored
// so downstream handlers can read it again.
func requestToJSON(r *http.Request) ([]byte, error) {
	decodedURL, err := url.QueryUnescape(r.URL.RequestURI())
	if err != nil {
		decodedURL = r.URL.RequestURI()
	}

	entry := requestLog{
		Method:   r.Method,
		URL:      decodedURL,
		ClientIP: clientIP(r),
		Headers:  safeHeaders(r),
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
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
				entry.Body = string(body) // keep raw on invalid JSON
			}

		case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
			body, err := readAndRestoreBody(r)
			if err != nil {
				entry.BodyError = err.Error()
				break
			}
			// ParseQuery directly avoids ParseForm's second body read + extra restore.
			if form, err := url.ParseQuery(string(body)); err == nil {
				entry.FormFields = flattenValues(form)
			}

		case strings.HasPrefix(ct, "multipart/form-data"):
			if err := r.ParseMultipartForm(maxMultipartSize); err != nil || r.MultipartForm == nil {
				break
			}
			if len(r.MultipartForm.Value) > 0 {
				entry.FormFields = flattenValues(r.MultipartForm.Value)
			}
			for field, fhs := range r.MultipartForm.File {
				for _, fh := range fhs {
					entry.UploadedFiles = append(entry.UploadedFiles, fileInfo{
						Field:       field,
						Filename:    fh.Filename,
						SizeBytes:   fh.Size,
						ContentType: fh.Header.Get("Content-Type"),
					})
				}
			}
		}
	}

	return json.Marshal(entry)
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
