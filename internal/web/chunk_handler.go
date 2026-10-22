package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/upload"
)

// handleChunkInit registers or resumes a chunked upload session.
// POST /{subpath}/chunk-init
// Form: uploadId (client-generated hash), filename, size, chunkSize (bytes)
// Response: 200 + {"uploadId":"...", "missingChunks":[0,1,...]}
func (s *Server) handleChunkInit(w http.ResponseWriter, r *http.Request) {
	fd, ok := s.resolveUploadTarget(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	uploadID := r.FormValue("uploadId")
	if !upload.ValidID(uploadID) {
		GoLog.Warnf("handleChunkInit: rejected uploadId %q from %s", uploadID, GoLog.IP(clientIP(r)))
		http.Error(w, "Bad Request: invalid uploadId", http.StatusBadRequest)
		return
	}
	filename := r.FormValue("filename")
	if filename == "" {
		http.Error(w, "Bad Request: missing filename", http.StatusBadRequest)
		return
	}
	size, errSize := strconv.ParseInt(r.FormValue("size"), 10, 64)
	chunkSize, errChunk := strconv.ParseInt(r.FormValue("chunkSize"), 10, 64)
	if errSize != nil || errChunk != nil || !s.chunkSizeAllowed(chunkSize, size) {
		http.Error(w, "Bad Request: invalid size or chunkSize", http.StatusBadRequest)
		return
	}

	missing, err := s.deps.Uploads.Init(uploadID, filename, size, chunkSize, fd.Path)
	if errors.Is(err, upload.ErrBadSize) {
		http.Error(w, "Bad Request: invalid size or chunkSize", http.StatusBadRequest)
		return
	}
	if err != nil {
		GoLog.Errorf("handleChunkInit: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"uploadId": uploadID, "missingChunks": missing})
}

// handleChunkReceive writes one chunk, sent as the raw request body, straight into the file: 202 while more are expected, 204 once the file is complete.
// POST /{subpath}/chunk?uploadId=…&chunkIndex=…
// A file that fits into one chunk may skip chunk-init by adding filename, size and chunkSize to the query.
func (s *Server) handleChunkReceive(w http.ResponseWriter, r *http.Request) {
	uploadID := r.URL.Query().Get("uploadId")
	if !upload.ValidID(uploadID) {
		GoLog.Warnf("handleChunkReceive: rejected uploadId %q from %s", uploadID, GoLog.IP(clientIP(r)))
		http.Error(w, "Bad Request: invalid uploadId", http.StatusBadRequest)
		return
	}
	chunkIndex, err := strconv.Atoi(r.URL.Query().Get("chunkIndex"))
	if err != nil || chunkIndex < 0 || chunkIndex >= upload.MaxTotalChunks {
		http.Error(w, "Bad Request: invalid chunkIndex", http.StatusBadRequest)
		return
	}

	fd, ok := s.resolveUploadTarget(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if q.Has("size") && !s.initSingleChunk(w, fd, uploadID, q) {
		return
	}

	done, err := s.deps.Uploads.Receive(uploadID, fd.Path, chunkIndex, r.Body)
	switch {
	case errors.Is(err, upload.ErrUnknownSession):
		http.Error(w, "Not Found: unknown upload session", http.StatusNotFound)
		return
	case errors.Is(err, upload.ErrChunkLength), errors.Is(err, upload.ErrBadIndex):
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	case err != nil:
		GoLog.Errorf("handleChunkReceive: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	s.stats().Visit(r.PathValue("subpath"), rateKey(r), r.UserAgent())
	if done {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusAccepted)
	}
}

// initSingleChunk registers the session for a file that fits into one chunk; it writes the error response and returns false when it cannot.
func (s *Server) initSingleChunk(w http.ResponseWriter, fd config.FileData, uploadID string, q url.Values) bool {
	size, errSize := strconv.ParseInt(q.Get("size"), 10, 64)
	chunkSize, errChunk := strconv.ParseInt(q.Get("chunkSize"), 10, 64)
	filename := q.Get("filename")
	if errSize != nil || errChunk != nil || filename == "" || size > chunkSize {
		http.Error(w, "Not Found: unknown upload session", http.StatusNotFound)
		return false
	}
	if !s.chunkSizeAllowed(chunkSize, size) {
		http.Error(w, "Bad Request: invalid size or chunkSize", http.StatusBadRequest)
		return false
	}
	if _, err := s.deps.Uploads.Init(uploadID, filename, size, chunkSize, fd.Path); err != nil {
		if errors.Is(err, upload.ErrBadSize) {
			http.Error(w, "Bad Request: invalid size or chunkSize", http.StatusBadRequest)
			return false
		}
		GoLog.Errorf("handleChunkReceive: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return false
	}
	return true
}

// chunkSizeAllowed reports whether chunks of chunkSize stay within the configured maximum POST size; a file that fits into one chunk only needs its own size to.
func (s *Server) chunkSizeAllowed(chunkSize, size int64) bool {
	limit := int64(s.deps.Store.Current().MaxPostSize())
	return min(chunkSize, max(size, 0)) <= limit
}
