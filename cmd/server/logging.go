// logging.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

type contextKey string

const (
	maxBodyBytes            = 1 << 20 // 1 MB
	multipartKey contextKey = "multipart"
	//TODO: this should be configurable
	logmaxLen = 4096
)

// parsedMultipart holds the result of a single multipart parse,
// shared between middleware, logger, and handler via context.
type parsedMultipart struct {
	Form  *multipart.Form
	Files []*multipart.FileHeader
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
	UploadedFiles []uploadedFile    `json:"uploaded_files,omitempty"`
}

type uploadedFile struct {
	Field       string `json:"field"`
	Filename    string `json:"filename"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type"`
}

// multipartFromContext retrieves the parsed multipart from the request context.
// Returns nil if the request was not a multipart upload.
func multipartFromContext(ctx context.Context) *parsedMultipart {
	pm, _ := ctx.Value(multipartKey).(*parsedMultipart)
	return pm
}

// multipartMiddleware parses multipart/form-data requests exactly once
// and stores the result in the request context for downstream handlers.
func multipartMiddleware(config *shared.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost &&
				strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				if err := r.ParseMultipartForm(int64(config.MaxPostSize)); err != nil {
					GoLog.Errorf("multipartMiddleware: failed to parse: %v", err)
					http.Error(w, "Bad Request", http.StatusBadRequest)
					return
				}
				pm := &parsedMultipart{
					Form:  r.MultipartForm,
					Files: r.MultipartForm.File["files"],
				}
				r = r.WithContext(context.WithValue(r.Context(), multipartKey, pm))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// loggingMiddleware logs every request. For multipart uploads it reads
// file metadata from the context instead of parsing the body again.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/log" {
			next.ServeHTTP(w, r)
			return
		}

		if data, err := buildRequestLog(r).toJSON(); err == nil {
			GoLog.Info(string(data))
		} else {
			GoLog.Warnf("loggingMiddleware: failed to serialize request: %v", err)
		}

		next.ServeHTTP(w, r)
	})
}

func (rl *requestLog) toJSON() ([]byte, error) {
	return json.Marshal(rl)
}

// buildRequestLog constructs a requestLog from the incoming request.
// Multipart metadata is read from context (already parsed by multipartMiddleware).
// Other body types are read and restored for downstream handlers.
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

	case strings.HasPrefix(ct, "multipart/form-data"):
		// Body already parsed by multipartMiddleware — read from context only.
		if pm := multipartFromContext(r.Context()); pm != nil {
			for _, fh := range pm.Files {
				entry.UploadedFiles = append(entry.UploadedFiles, uploadedFile{
					Field:       "files",
					Filename:    fh.Filename,
					SizeBytes:   fh.Size,
					ContentType: fh.Header.Get("Content-Type"),
				})
			}
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
