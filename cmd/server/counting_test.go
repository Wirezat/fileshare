package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/pkg/shared"
)

func usesLeft(t *testing.T) int {
	t.Helper()
	return shareOf(t, "docs").Uses
}

func visit(t *testing.T, method, target string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	handleRequest(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if strings.HasPrefix(c.Name, "session_") {
			return c.Name + "=" + c.Value
		}
	}
	return ""
}

func TestFolderShareCountsOneVisitPerSession(t *testing.T) {
	withOfficeShare(t, shared.FileData{Uses: 3}, "", "")

	first := visit(t, http.MethodGet, "/docs/notiz.txt")
	if first.Code != http.StatusOK || usesLeft(t) != 2 {
		t.Fatalf("deep link: status %d, uses %d, want 200 and 2", first.Code, usesLeft(t))
	}
	ck := sessionCookie(first)
	if ck == "" {
		t.Fatal("first visit set no session cookie")
	}

	visit(t, http.MethodGet, "/docs/notiz.txt", "Cookie", ck)
	visit(t, http.MethodGet, "/docs/bericht.docx", "Cookie", ck)
	visit(t, http.MethodGet, "/docs/bericht.docx?dl=1", "Cookie", ck)
	if usesLeft(t) != 2 {
		t.Errorf("further requests in the same session counted: uses %d", usesLeft(t))
	}

	visit(t, http.MethodGet, "/docs/notiz.txt")
	if usesLeft(t) != 1 {
		t.Errorf("a new session did not count: uses %d", usesLeft(t))
	}
}

func TestFileShareCountsOneVisitPerSession(t *testing.T) {
	file := withSingleFileShare(t, "")
	_ = file
	cfg, _ := shared.LoadConfig()
	fd := cfg.Files["vertrag"]
	fd.Uses = 3
	fd.Office = shared.OfficeOff
	cfg.Files["vertrag"] = fd
	shared.SaveConfig(cfg)

	first := visit(t, http.MethodGet, "/vertrag")
	ck := sessionCookie(first)
	if ck == "" {
		t.Fatal("file share visit set no session cookie")
	}
	for i := 0; i < 5; i++ {
		visit(t, http.MethodGet, "/vertrag", "Cookie", ck, "Range", "bytes=0-3")
	}
	cfg, _ = shared.LoadConfig()
	if got := cfg.Files["vertrag"].Uses; got != 2 {
		t.Errorf("uses = %d after one session with six requests, want 2", got)
	}
}

func TestHeadIsNotAVisit(t *testing.T) {
	withOfficeShare(t, shared.FileData{Uses: 3}, "", "")

	rec := visit(t, http.MethodHead, "/docs/notiz.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD status %d", rec.Code)
	}
	if usesLeft(t) != 3 {
		t.Errorf("HEAD counted: uses %d", usesLeft(t))
	}
	if sessionCookie(rec) != "" {
		t.Error("HEAD set a session cookie")
	}
}

func TestDocumentServerFetchBelongsToTheVisit(t *testing.T) {
	withOfficeShare(t, shared.FileData{Uses: 3, Office: shared.OfficeView}, "https://office.example.com", testSecret)
	exp := time.Now().Add(5 * time.Minute).Unix()

	page := visit(t, http.MethodGet, "/docs/bericht.docx")
	if !strings.Contains(page.Body.String(), "api.js") || usesLeft(t) != 2 {
		t.Fatalf("viewer page: uses %d, want 2", usesLeft(t))
	}

	fetch := visit(t, http.MethodGet, "/docs/bericht.docx?dl=1",
		"Authorization", dsBearer(t, testSecret, "https://fs.example.com/docs/bericht.docx?dl=1", exp))
	if fetch.Body.String() != docBytes {
		t.Fatalf("document server got %q", fetch.Body.String())
	}
	if usesLeft(t) != 2 {
		t.Errorf("the document server's fetch counted as a visit: uses %d", usesLeft(t))
	}
	if sessionCookie(fetch) != "" {
		t.Error("the document server was handed a session cookie")
	}
}

func TestLastVisitExpiresTheShare(t *testing.T) {
	withOfficeShare(t, shared.FileData{Uses: 1}, "", "")

	if rec := visit(t, http.MethodGet, "/docs/notiz.txt"); rec.Code != http.StatusOK {
		t.Fatalf("last visit: %d", rec.Code)
	}
	if !shareOf(t, "docs").Expired {
		t.Error("share not marked expired after the last visit")
	}
	if rec := visit(t, http.MethodGet, "/docs/notiz.txt"); rec.Code != http.StatusGone {
		t.Errorf("after expiry: %d, want 410", rec.Code)
	}
}

func TestUnlimitedSharesNeverTouchTheConfig(t *testing.T) {
	withOfficeShare(t, shared.FileData{Uses: -1}, "", "")
	before := shareOf(t, "docs")

	rec := visit(t, http.MethodGet, "/docs/notiz.txt")
	if sessionCookie(rec) != "" {
		t.Error("unlimited share set a session cookie for nothing")
	}
	if shareOf(t, "docs") != before {
		t.Error("unlimited share was rewritten")
	}
}
