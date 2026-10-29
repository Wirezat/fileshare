package webhook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/upload"
)

func viewOf(server string, files map[string]config.FileData) func() config.View {
	return func() config.View {
		return config.NewView(&config.Config{WebhookServer: server, WebhookTopic: "topic", Files: files})
	}
}

func TestPublishedPostsNameSizeAndShare(t *testing.T) {
	var title, body, method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		title, body, method, path = r.Header.Get("Title"), string(b), r.Method, r.URL.Path
	}))
	defer srv.Close()
	files := map[string]config.FileData{
		"urlaub": {Path: "/srv/urlaub"},
		"rest":   {Path: "/srv"},
	}
	New(viewOf(srv.URL, files)).Published(upload.Published{Filename: "foto.jpg", DestDir: "/srv/urlaub/2026", Size: 4404019})

	if path != "/topic" {
		t.Errorf("path = %q, want /topic", path)
	}
	if method != http.MethodPost || title != "Upload in urlaub" || body != "foto.jpg (4.2 MB)\n2026" {
		t.Errorf("got %s title=%q body=%q", method, title, body)
	}
}

func TestPublishedSendsTokenAsBearer(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
	}))
	defer srv.Close()
	view := func() config.View {
		return config.NewView(&config.Config{WebhookServer: srv.URL, WebhookTopic: "topic", WebhookToken: "abc123"})
	}
	New(view).Published(upload.Published{Filename: "x", DestDir: "/srv/a", Size: 1})
	if auth != "Bearer abc123" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestPublishedNamesShareRootWithoutSubfolder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()
	files := map[string]config.FileData{"a": {Path: "/srv/a"}}
	New(viewOf(srv.URL, files)).Published(upload.Published{Filename: "x", DestDir: "/srv/a", Size: 10})
	if body != "x (10 B)" {
		t.Errorf("body = %q", body)
	}
}

func TestPublishedWithoutTopicSendsNothing(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	view := func() config.View { return config.NewView(&config.Config{WebhookServer: srv.URL}) }
	New(view).Published(upload.Published{Filename: "x", DestDir: "/srv/a", Size: 1})
	New(viewOf("", nil)).Published(upload.Published{Filename: "x", DestDir: "/srv/a", Size: 1})
	if hit {
		t.Error("request sent without topic or server")
	}
}

func TestPublishedSurvivesFailingServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	url := srv.URL
	srv.Close()
	New(viewOf(url, nil)).Published(upload.Published{Filename: "x", DestDir: "/srv/a", Size: 1})
}
