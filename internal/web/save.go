package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/atomicfile"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/filetype"
)

// fileETag returns the entity tag of a file version, built from its modification time and size.
func fileETag(info os.FileInfo) string {
	return fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size())
}

// isTextFile reports whether the requested file is text: by its extension, or by its first bytes when the
// extension is unknown.
func isTextFile(ctx *requestContext) bool {
	if filetype.Lookup(typedName(ctx)).Kind == filetype.Text {
		return true
	}
	if fileView(ctx) != filetype.ViewProbe {
		return false
	}
	f, err := os.Open(ctx.diskPath)
	if err != nil {
		return false
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, firstLinesRead))
	return filetype.Sniff(head)
}

// handleSave replaces the requested text file with the request body. It needs a share in office mode edit, a
// browser rather than the document server, an
// If-Match header with the current entity tag of the file and a body of at most the upload size limit; it answers
// 204 with the new entity tag, or 403, 412, 413, 415 or 428.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	switch {
	case ctx.machine || ctx.fileData.Office != config.OfficeEdit:
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	case ctx.fileInfo.IsDir() || !isTextFile(ctx):
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		return
	case r.Header.Get("If-Match") == "":
		http.Error(w, "Precondition Required", http.StatusPreconditionRequired)
		return
	}
	limit := int64(ctx.config.MaxPostSize())
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Bad Request", http.StatusBadRequest)
		}
		return
	}

	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	info, err := os.Stat(ctx.diskPath)
	if err != nil || fileETag(info) != r.Header.Get("If-Match") {
		http.Error(w, "Precondition Failed", http.StatusPreconditionFailed)
		return
	}
	if err := atomicfile.Replace(ctx.diskPath, bytes.NewReader(body), limit); err != nil {
		GoLog.Errorf("save /%s%s: %v", ctx.subpath, sharePath(ctx), err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if info, err := os.Stat(ctx.diskPath); err == nil {
		w.Header().Set("ETag", fileETag(info))
	}
	GoLog.Infof("text saved: /%s%s (%d bytes)", ctx.subpath, sharePath(ctx), len(body))
	s.stats().Saved(ctx.subpath)
	w.WriteHeader(http.StatusNoContent)
}
