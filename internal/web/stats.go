package web

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Wirezat/fileshare/internal/media"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/stats"
)

type statsService interface {
	Visit(share, client, userAgent string)
	PageView(userAgent string)
	Download(d stats.Download) func(sent int64, complete bool)
	Report(exists func(share string) bool) stats.Report
}

type noStats struct{}

func (noStats) Visit(string, string, string) {}
func (noStats) PageView(string)              {}
func (noStats) Download(stats.Download) func(int64, bool) {
	return func(int64, bool) {}
}
func (noStats) Report(func(string) bool) stats.Report { return stats.Report{} }

func (s *Server) stats() statsService {
	if s.deps.Stats == nil {
		return noStats{}
	}
	return s.deps.Stats
}

// fromDocServer reports whether the request carries the document server's token for this URL.
func fromDocServer(r *http.Request, ctx *requestContext) bool {
	return ctx.config.OfficeReady() &&
		office.TokenAllows(r.Header.Get("Authorization"), r.URL.RequestURI(), ctx.config.OfficeSecret())
}

// isPageLoad reports whether the request opens a page rather than loading media or a part of a file.
func isPageLoad(r *http.Request) bool {
	if dest := r.Header.Get("Sec-Fetch-Dest"); dest != "" {
		return dest == "document"
	}
	return r.Header.Get("Range") == ""
}

// shownInline reports whether a browser displays name instead of saving it.
func shownInline(name string) bool {
	mt := media.MimeType(name)
	for _, prefix := range []string{"image/", "video/", "audio/", "text/", "application/pdf"} {
		if strings.HasPrefix(mt, prefix) {
			return true
		}
	}
	return media.IsText(name)
}

// isFileDownload reports whether serving this file counts as a download: the download button,
// or a file the browser saves instead of showing.
func isFileDownload(r *http.Request, ctx *requestContext) bool {
	return r.Method == http.MethodGet && (r.URL.Query().Get("dl") != "" || !shownInline(ctx.fileInfo.Name()))
}

// resumes reports whether the request asks for a range that does not start at byte 0.
func resumes(r *http.Request) bool {
	spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes=")
	if !ok {
		return false
	}
	first, _, _ := strings.Cut(spec, ",")
	start, _, _ := strings.Cut(strings.TrimSpace(first), "-")
	n, err := strconv.ParseInt(start, 10, 64)
	return err != nil || n != 0
}

// sharePath returns the path of the served file inside its share, or the file's name for a file share.
func sharePath(ctx *requestContext) string {
	if rel := strings.TrimPrefix(ctx.diskPath, ctx.fileData.Path); rel != "" {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(ctx.diskPath)
}

// downloadWriter counts the body bytes of a download and starts recording it once a 200 or 206 goes out.
type downloadWriter struct {
	http.ResponseWriter
	stats  statsService
	d      stats.Download
	end    func(int64, bool)
	want   int64
	sent   int64
	err    error
	header bool
}

func (w *downloadWriter) WriteHeader(code int) {
	if !w.header {
		w.header = true
		if code == http.StatusOK || code == http.StatusPartialContent {
			w.want, _ = strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
			if w.Header().Get("Content-Length") == "" {
				w.want = -1
			}
			w.d.Resumed = w.d.Resumed && code == http.StatusPartialContent
			w.end = w.stats.Download(w.d)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *downloadWriter) Write(p []byte) (int, error) {
	if !w.header {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.sent += int64(n)
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}

func (w *downloadWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// finish records the download; returned is false when the handler did not get to its end.
func (w *downloadWriter) finish(returned bool) {
	if w.end == nil {
		return
	}
	complete := returned && w.err == nil && (w.want < 0 || w.sent == w.want)
	w.end(w.sent, complete)
}

// countDownload wraps w so the response is recorded as d once it starts.
func (s *Server) countDownload(w http.ResponseWriter, d stats.Download) *downloadWriter {
	return &downloadWriter{ResponseWriter: w, stats: s.stats(), d: d}
}
