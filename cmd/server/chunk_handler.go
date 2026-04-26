package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// handleChunkInit registers or resumes a chunked upload session.
// POST /{subpath}/chunk-init
// Form: uploadId (client-generated hash), filename, totalChunks
// Response: 200 + {"uploadId":"...", "missingChunks":[0,1,...]}
func handleChunkInit(w http.ResponseWriter, r *http.Request) {
	fd, ok := resolveUploadTarget(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	uploadID := r.FormValue("uploadId")
	if uploadID == "" {
		http.Error(w, "Bad Request: missing uploadId", http.StatusBadRequest)
		return
	}
	filename := r.FormValue("filename")
	if filename == "" {
		http.Error(w, "Bad Request: missing filename", http.StatusBadRequest)
		return
	}
	totalChunks, err := strconv.Atoi(r.FormValue("totalChunks"))
	if err != nil || totalChunks < 1 {
		http.Error(w, "Bad Request: invalid totalChunks", http.StatusBadRequest)
		return
	}

	missing, err := storage.InitChunk(uploadID, filename, totalChunks, fd.Path)
	if err != nil {
		GoLog.Errorf("handleChunkInit: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"uploadId": uploadID, "missingChunks": missing})
}

// handleChunkReceive stores a single chunk. Returns 202 while more chunks
// are expected; 204 when the file has been fully assembled.
// POST /{subpath}/chunk
// Multipart: uploadId, chunkIndex, chunk (file)
func handleChunkReceive(w http.ResponseWriter, r *http.Request) {
	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("handleChunkReceive: load config: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := r.ParseMultipartForm(int64(config.MaxPostSize)); err != nil {
		GoLog.Errorf("handleChunkReceive: parse multipart: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	uploadID := r.FormValue("uploadId")
	chunkIndex, err := strconv.Atoi(r.FormValue("chunkIndex"))
	if err != nil || chunkIndex < 0 {
		http.Error(w, "Bad Request: invalid chunkIndex", http.StatusBadRequest)
		return
	}

	f, _, err := r.FormFile("chunk")
	if err != nil {
		http.Error(w, "Bad Request: missing chunk", http.StatusBadRequest)
		return
	}
	defer f.Close()

	done, err := storage.ReceiveChunk(uploadID, chunkIndex, f)
	if err != nil {
		GoLog.Errorf("handleChunkReceive: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if done {
		w.WriteHeader(http.StatusNoContent) // 204 — upload complete
	} else {
		w.WriteHeader(http.StatusAccepted) // 202 — more chunks expected
	}
}
