package web

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/stats"
)

const firefox = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"

func asBrowser(r *http.Request) { r.Header.Set("User-Agent", firefox) }

func fromClient(ip string) func(*http.Request) {
	return func(r *http.Request) { r.RemoteAddr = net.JoinHostPort(ip, "40000") }
}

// statsShares serves "docs" (a folder with a.txt, data.bin and photo.jpg) and "single" (just data.bin).
func statsShares(t *testing.T, extra map[string]config.FileData) (*testEnv, string) {
	t.Helper()
	dir := shareDir(t)
	os.WriteFile(filepath.Join(dir, "data.bin"), []byte("0123456789"), 0o600)
	os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("not really a jpeg"), 0o600)
	shares := map[string]config.FileData{
		"docs":   {Path: dir},
		"single": {Path: filepath.Join(dir, "data.bin")},
	}
	for k, v := range extra {
		shares[k] = v
	}
	return publicEnv(t, shares), dir
}

func report(e *testEnv) stats.Period {
	return e.stats.Report(func(string) bool { return true }).Total
}

func TestOnlyDownloadCommandsCountAsDownloads(t *testing.T) {
	e, _ := statsShares(t, nil)
	h := e.handler
	for _, target := range []string{
		"/docs", "/docs/a.txt", "/docs/a.txt?view=text", "/docs/photo.jpg", "/docs/photo.jpg?preview=1", "/docs/sub",
	} {
		send(h, http.MethodGet, target, nil, asBrowser)
	}
	if p := report(e); p.Downloads != 0 || p.BytesDown != 0 {
		t.Fatalf("viewing counted as download: %d downloads, %d bytes", p.Downloads, p.BytesDown)
	}

	send(h, http.MethodGet, "/docs/a.txt?dl=1", nil, asBrowser)
	send(h, http.MethodGet, "/docs/data.bin", nil, asBrowser)
	send(h, http.MethodGet, "/single", nil, asBrowser)
	send(h, http.MethodHead, "/docs/a.txt?dl=1", nil, asBrowser)
	p := report(e)
	if p.Downloads != 3 || p.BytesDown != 5+10+10 || p.DownloadsIncomplete != 0 {
		t.Errorf("downloads %d bytes %d incomplete %d, want 3 25 0", p.Downloads, p.BytesDown, p.DownloadsIncomplete)
	}
	files := map[string]int64{}
	for _, f := range p.TopFiles {
		files[f.Share+":"+f.Path] = f.Downloads
	}
	if files["docs:/a.txt"] != 1 || files["docs:/data.bin"] != 1 || files["single:data.bin"] != 1 {
		t.Errorf("top files %v", files)
	}
}

func TestZipCountsAsZipDownload(t *testing.T) {
	e, _ := statsShares(t, nil)
	rec := send(e.handler, http.MethodGet, "/docs?download=zip", nil, asBrowser)
	send(e.handler, http.MethodHead, "/docs?download=zip", nil, asBrowser)
	p := report(e)
	if p.Downloads != 1 || p.ZipDownloads != 1 || p.BytesDown != int64(rec.Body.Len()) {
		t.Errorf("downloads %d zip %d bytes %d, want 1 1 %d", p.Downloads, p.ZipDownloads, p.BytesDown, rec.Body.Len())
	}
	if len(p.TopFiles) != 0 || len(p.TopShares) != 1 || p.TopShares[0].Downloads != 1 {
		t.Errorf("top files %v, top shares %v", p.TopFiles, p.TopShares)
	}
}

func TestRangeFromZeroIsADownloadOtherRangesOnlyBytes(t *testing.T) {
	e, _ := statsShares(t, nil)
	send(e.handler, http.MethodGet, "/docs/data.bin?dl=1", nil, asBrowser, withHeader("Range", "bytes=0-3"))
	send(e.handler, http.MethodGet, "/docs/data.bin?dl=1", nil, asBrowser, withHeader("Range", "bytes=4-"))
	send(e.handler, http.MethodGet, "/docs/data.bin?dl=1", nil, asBrowser, withHeader("Range", "bytes=-2"))
	send(e.handler, http.MethodGet, "/docs/data.bin?dl=1", nil, asBrowser, withHeader("Range", "bytes=50-"))
	p := report(e)
	if p.Downloads != 1 || p.BytesDown != 4+6+2 || p.DownloadsIncomplete != 0 {
		t.Errorf("downloads %d bytes %d incomplete %d, want 1 12 0", p.Downloads, p.BytesDown, p.DownloadsIncomplete)
	}
}

func TestRangeAnsweredInFullIsADownload(t *testing.T) {
	e, _ := statsShares(t, nil)
	rec := send(e.handler, http.MethodGet, "/docs/data.bin?dl=1", nil, asBrowser,
		withHeader("Range", "bytes=4-"), withHeader("If-Range", "Mon, 01 Jan 2001 00:00:00 GMT"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want the whole file", rec.Code)
	}
	if p := report(e); p.Downloads != 1 || p.BytesDown != 10 {
		t.Errorf("downloads %d bytes %d, want 1 and 10", p.Downloads, p.BytesDown)
	}
}

func TestListingLinksForcedDownloadsAsDownloads(t *testing.T) {
	e, _ := statsShares(t, nil)
	body := send(e.handler, http.MethodGet, "/docs", nil, asBrowser).Body.String()
	for _, want := range []string{`href="/docs/photo.jpg?dl=1" download`, `href="/docs/data.bin?dl=1" download`, `href="/docs/a.txt">`, `href="/docs/sub">`} {
		if !strings.Contains(body, want) {
			t.Errorf("listing lacks %s", want)
		}
	}
	if strings.Contains(body, `href="/docs/photo.jpg" download`) {
		t.Error("a forced download links to the file without ?dl=1")
	}
	send(e.handler, http.MethodGet, "/docs/photo.jpg?dl=1", nil, asBrowser)
	if p := report(e); p.Downloads != 1 {
		t.Errorf("downloads %d, want 1", p.Downloads)
	}
}

func TestResumes(t *testing.T) {
	for rng, want := range map[string]bool{
		"": false, "bytes=0-": false, "bytes=0-99": false, "bytes=0-1,5-9": false, "bytes= 0-5": false,
		"bytes=1-": true, "bytes=-500": true, "bytes=5-9,0-1": true, "items=0-1": false, "bytes=x-": true,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Range", rng)
		if got := resumes(r); got != want {
			t.Errorf("resumes(%q) = %v, want %v", rng, got, want)
		}
	}
}

type failingWriter struct {
	*httptest.ResponseRecorder
	left int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if len(p) > w.left {
		n, _ := w.ResponseRecorder.Write(p[:w.left])
		w.left = 0
		return n, errors.New("client gone")
	}
	w.left -= len(p)
	return w.ResponseRecorder.Write(p)
}

func TestBrokenOffDownloadIsIncomplete(t *testing.T) {
	e, _ := statsShares(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/docs/data.bin?dl=1", nil)
	asBrowser(req)
	e.srv.handleRequest(&failingWriter{httptest.NewRecorder(), 4}, req)

	req = httptest.NewRequest(http.MethodGet, "/docs?download=zip", nil)
	asBrowser(req)
	e.srv.handleRequest(&failingWriter{httptest.NewRecorder(), 30}, req)

	p := report(e)
	if p.Downloads != 2 || p.DownloadsIncomplete != 2 || p.BytesDown != 4+30 {
		t.Errorf("downloads %d incomplete %d bytes %d, want 2 2 34", p.Downloads, p.DownloadsIncomplete, p.BytesDown)
	}
}

func TestBotsAndScriptsAreNotCounted(t *testing.T) {
	e, _ := statsShares(t, nil)
	for _, ua := range []string{"", "WhatsApp/2.23", "curl/8.5.0"} {
		ua := withHeader("User-Agent", ua)
		send(e.handler, http.MethodGet, "/docs", nil, ua)
		send(e.handler, http.MethodGet, "/docs/a.txt?dl=1", nil, ua)
		send(e.handler, http.MethodGet, "/single", nil, ua)
	}
	p := report(e)
	if p.Visitors != 0 || p.Downloads != 0 || p.BytesDown != 0 || len(p.TopShares) != 0 {
		t.Errorf("bots counted: visitors %d downloads %d bytes %d shares %v", p.Visitors, p.Downloads, p.BytesDown, p.TopShares)
	}
	if p.Devices[stats.DeviceBot] != 6 || len(p.Browsers) != 0 {
		t.Errorf("devices %v browsers %v, want 6 bot page views", p.Devices, p.Browsers)
	}
}

func TestVisitorsCountOnlySuccessfulShareAccess(t *testing.T) {
	hash, _ := password.Hash("pw")
	e, dir := statsShares(t, nil)
	e.store.Update(func(c *config.Config) error {
		c.Files["locked"] = config.FileData{Path: dir, Password: hash}
		c.Files["old"] = config.FileData{Path: dir, Expired: true}
		return nil
	})
	for _, target := range []string{"/missing", "/locked", "/old", "/docs/nope.txt", "/static/share.css"} {
		send(e.handler, http.MethodGet, target, nil, asBrowser, fromClient("203.0.113.9"))
	}
	if p := report(e); p.Visitors != 0 || len(p.TopShares) != 0 {
		t.Fatalf("failed access counted: visitors %d, shares %v", p.Visitors, p.TopShares)
	}
	send(e.handler, http.MethodGet, "/docs", nil, asBrowser, fromClient("203.0.113.9"))
	send(e.handler, http.MethodGet, "/docs/a.txt", nil, asBrowser, fromClient("203.0.113.9"))
	send(e.handler, http.MethodGet, "/docs/a.txt", nil, asBrowser, fromClient("2001:db8:1:2::1"))
	send(e.handler, http.MethodGet, "/single", nil, asBrowser, fromClient("2001:db8:1:2::2"))
	p := report(e)
	if p.Visitors != 2 {
		t.Errorf("visitors %d, want 2 (one IPv4, one IPv6 /64)", p.Visitors)
	}
	rows := map[string]stats.ShareRow{}
	for _, r := range p.TopShares {
		rows[r.Name] = r
	}
	if rows["docs"].Visitors != 2 || rows["single"].Visitors != 1 || time.Since(rows["docs"].LastAccess) > time.Minute {
		t.Errorf("share rows %v", rows)
	}
}

func TestPageViewsAreShareOpensOnly(t *testing.T) {
	e, _ := statsShares(t, nil)
	send(e.handler, http.MethodGet, "/docs", nil, asBrowser)
	send(e.handler, http.MethodGet, "/docs/sub", nil, asBrowser)
	send(e.handler, http.MethodGet, "/single", nil, asBrowser)
	send(e.handler, http.MethodGet, "/docs/a.txt", nil, asBrowser)
	send(e.handler, http.MethodGet, "/docs/photo.jpg?preview=1", nil, asBrowser)
	send(e.handler, http.MethodGet, "/single", nil, asBrowser, withHeader("Range", "bytes=0-"))
	send(e.handler, http.MethodGet, "/single", nil, asBrowser, withHeader("Sec-Fetch-Dest", "video"))
	send(e.handler, http.MethodGet, "/docs?download=zip", nil, asBrowser)
	send(e.handler, http.MethodHead, "/docs", nil, asBrowser)
	if p := report(e); p.Devices[stats.DeviceDesktop] != 3 || p.Browsers[stats.BrowserFirefox] != 3 {
		t.Errorf("devices %v browsers %v, want 3 page views", p.Devices, p.Browsers)
	}
}

func TestDocumentServerFetchIsNotCounted(t *testing.T) {
	e, _ := withOfficeShare(t, config.FileData{Office: config.OfficeView}, "https://office.example.com", testSecret)
	exp := time.Now().Add(5 * time.Minute).Unix()
	rec := get(t, e, "/docs/bericht.docx?dl=1", "User-Agent", "Node.js/18",
		"Authorization", dsBearer(t, testSecret, "https://fileshare.example.com/docs/bericht.docx?dl=1", exp))
	if rec.Code != http.StatusOK {
		t.Fatalf("document server fetch: %d", rec.Code)
	}
	if p := report(e); p.Downloads != 0 || p.Visitors != 0 || len(p.Devices) != 0 {
		t.Errorf("document server counted: %+v", p)
	}
	get(t, e, "/docs/bericht.docx?dl=1", "User-Agent", firefox)
	if p := report(e); p.Downloads != 1 {
		t.Errorf("download button: %d downloads, want 1", p.Downloads)
	}
}

func TestChunkUploadCountsVisitorAndUpload(t *testing.T) {
	dir := shareDir(t)
	e := publicEnv(t, map[string]config.FileData{"up": {Path: dir, AllowPost: true}})
	h := e.handler
	if got := chunkInit(h, "up"); got != http.StatusOK {
		t.Fatalf("init: %d", got)
	}
	body := strings.Repeat("x", testChunk)
	send(h, http.MethodPost, "/up/chunk?uploadId=upload-0001&chunkIndex=0", strings.NewReader(body), asBrowser)
	send(h, http.MethodPost, "/up/chunk?uploadId=upload-0001&chunkIndex=1", strings.NewReader("abc"), asBrowser)
	p := report(e)
	if p.Uploads != 1 || p.BytesUp != testChunk+3 || p.Visitors != 1 {
		t.Errorf("uploads %d bytes %d visitors %d, want 1 %d 1", p.Uploads, p.BytesUp, p.Visitors, testChunk+3)
	}
}

type panickingWriter struct{ *httptest.ResponseRecorder }

func (panickingWriter) Write([]byte) (int, error) { panic("handler blew up") }

func TestPanickingDownloadStillEnds(t *testing.T) {
	e, _ := statsShares(t, nil)
	for _, target := range []string{"/docs/data.bin?dl=1", "/docs?download=zip"} {
		func() {
			defer func() { recover() }()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			asBrowser(req)
			e.srv.handleRequest(panickingWriter{httptest.NewRecorder()}, req)
		}()
	}
	r := e.stats.Report(func(string) bool { return true })
	if r.Live != 0 || r.Total.Downloads != 2 || r.Total.DownloadsIncomplete != 2 {
		t.Errorf("live %d downloads %d incomplete %d, want 0 2 2", r.Live, r.Total.Downloads, r.Total.DownloadsIncomplete)
	}
}

func TestShownInline(t *testing.T) {
	for name, want := range map[string]bool{
		"a.jpg": true, "b.mp4": true, "c.mp3": true, "d.pdf": true, "e.txt": true, "f.md": true, "g.go": true,
		"h.zip": false, "i.iso": false, "j.docx": false, "k": false, "l.exe": false,
	} {
		if got := shownInline(name); got != want {
			t.Errorf("shownInline(%q) = %v, want %v", name, got, want)
		}
	}
}
