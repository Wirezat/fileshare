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

// sensitiveHeaders contains Header-Names
// that should be excluded from Logs.
var sensitiveHeaders = map[string]bool{
	http.CanonicalHeaderKey("authorization"): true,
	http.CanonicalHeaderKey("cookie"):        true,
	http.CanonicalHeaderKey("x-auth-token"):  true,
}

// clientIP extracts the real client IP from the request.
// Order: X-Forwarded-For (first entry) → Cf-Connecting-Ip → RemoteAddr.
// Splitting by "," correctly handles proxy chains.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain "client, proxy1, proxy2", only the first (original) IP is relevant.
		return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
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

// safeHeaders returns a copy of the request headers without sensitive fields.
func safeHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string, len(r.Header))
	for name, values := range r.Header {
		if sensitiveHeaders[name] {
			continue
		}
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}
	return headers
}

// requestToJSON serializes an HTTP request as an indented JSON string.
//
// Included fields:
//   - method, url, client_ip, headers (sensitive headers filtered)
//   - body          – for POST/PUT with JSON or form body
//   - form_fields   – text fields for multipart/form-data
//   - uploaded_files – file metadata for multipart/form-data (no file content)
//
// The request body is always reset after reading, so that
// subsequent handlers can read it again.
func requestToJSON(r *http.Request) ([]byte, error) {
	decodedURL, err := url.QueryUnescape(r.URL.RequestURI())
	if err != nil {
		decodedURL = r.URL.RequestURI()
	}

	logData := map[string]interface{}{
		"method":    r.Method,
		"url":       decodedURL,
		"client_ip": clientIP(r),
		"headers":   safeHeaders(r),
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		contentType := r.Header.Get("Content-Type")

		switch {
		case strings.HasPrefix(contentType, "application/json"):
			body, bodyErr := readAndRestoreBody(r)
			if bodyErr != nil {
				logData["body_error"] = bodyErr.Error()
				break
			}
			var parsed interface{}
			if json.Unmarshal(body, &parsed) == nil {
				logData["body"] = parsed
			} else {
				// Invalid JSON – log raw string
				logData["body"] = string(body)
			}

		case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
			// Read and restore body first, so that ParseForm can find it
			// and subsequent handlers can also read it.
			body, bodyErr := readAndRestoreBody(r)
			if bodyErr != nil {
				logData["body_error"] = bodyErr.Error()
				break
			}
			_ = r.ParseForm()
			logData["body"] = r.Form
			// Restore body after ParseForm
			r.Body = io.NopCloser(bytes.NewReader(body))

		case strings.HasPrefix(contentType, "multipart/form-data"):
			if parseErr := r.ParseMultipartForm(maxMultipartSize); parseErr == nil && r.MultipartForm != nil {
				// Text form fields
				if len(r.MultipartForm.Value) > 0 {
					formFields := make(map[string]string, len(r.MultipartForm.Value))
					for field, values := range r.MultipartForm.Value {
						if len(values) > 0 {
							formFields[field] = values[0]
						}
					}
					logData["form_fields"] = formFields
				}

				// file metadata
				var files []map[string]interface{}
				for field, fhs := range r.MultipartForm.File {
					for _, fh := range fhs {
						files = append(files, map[string]interface{}{
							"field":        field,
							"filename":     fh.Filename,
							"size_bytes":   fh.Size,
							"content_type": fh.Header.Get("Content-Type"),
						})
					}
				}
				if len(files) > 0 {
					logData["uploaded_files"] = files
				}
			}
		}
	}

	return json.MarshalIndent(logData, "", "  ")
}

// readAndRestoreBody reads the request body (limited to maxBodyBytes),
// resets it afterwards, and returns the read content.
func readAndRestoreBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return body, nil
}
