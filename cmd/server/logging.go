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

// requestToJSON serializes an HTTP request to a JSON string for logging.
func requestToJSON(r *http.Request) (string, error) {
	decodedURL, err := url.QueryUnescape(r.URL.RequestURI())
	if err != nil {
		return "", err
	}

	getIP := func() string {
		if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
			return ip
		}
		if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" {
			return ip
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			return host
		}
		return r.RemoteAddr
	}

	// Sensitive Header vor dem Loggen entfernen
	headers := make(map[string]string)
	sensitiveHeaders := map[string]bool{
		"Authorization": true,
		"Cookie":        true,
		"X-Auth-Token":  true,
	}
	for name, values := range r.Header {
		if sensitiveHeaders[name] {
			continue
		}
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}

	var bodyContent interface{}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil {
				var jsonBody interface{}
				if json.Unmarshal(bodyBytes, &jsonBody) == nil {
					bodyContent = jsonBody
				} else {
					bodyContent = string(bodyBytes)
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		} else if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
			_ = r.ParseForm()
			bodyContent = r.Form
		}
	}

	var files []map[string]interface{}
	if r.Method == http.MethodPost && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err == nil && r.MultipartForm != nil {
			for field, fhs := range r.MultipartForm.File {
				for _, fh := range fhs {
					files = append(files, map[string]interface{}{
						"field":       field,
						"filename":    fh.Filename,
						"size_bytes":  fh.Size,
						"contenttype": fh.Header.Get("Content-Type"),
					})
				}
			}
		}
	}

	logData := map[string]interface{}{
		"method":    r.Method,
		"url":       decodedURL,
		"client_ip": getIP(),
		"headers":   headers,
	}
	if bodyContent != nil {
		logData["body"] = bodyContent
	}
	if len(files) > 0 {
		logData["uploaded_files"] = files
	}

	jsonBytes, err := json.MarshalIndent(logData, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}
