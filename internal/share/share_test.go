package share

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/store"
)

const testSecret = "06e7f94b2b06ce5c7fc22730c5639b6eb1cab0a36725a9648e6396546ae2fdca"

func newService(t *testing.T, shares map[string]config.FileData) (*Service, *store.FileStore) {
	t.Helper()
	data, err := json.Marshal(config.Config{AdminUsername: "admin", OfficeSecret: testSecret, Files: shares})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return New(st), st
}

func TestResolveFindsTheFile(t *testing.T) {
	s, _ := newService(t, map[string]config.FileData{"docs": {Path: "/srv/docs"}})
	res, err := s.Resolve("docs", "/sub/b.txt", Access{})
	if err != nil {
		t.Fatal(err)
	}
	if res.DiskPath != "/srv/docs/sub/b.txt" || res.Subpath != "docs" || res.Share.Path != "/srv/docs" {
		t.Errorf("resolved = %+v", res)
	}
	if res, _ := s.Resolve("docs", "", Access{}); res.DiskPath != "/srv/docs" {
		t.Errorf("share root = %q", res.DiskPath)
	}
}

func TestResolveUnknownShare(t *testing.T) {
	s, _ := newService(t, nil)
	if _, err := s.Resolve("nope", "/a.txt", Access{}); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestResolveRejectsPathsOutsideTheShare(t *testing.T) {
	s, _ := newService(t, map[string]config.FileData{"docs": {Path: "/srv/docs"}})
	for _, rel := range []string{"/../secret.txt", "/../docs-other/x", "/sub/../../etc/passwd"} {
		if _, err := s.Resolve("docs", rel, Access{}); err != ErrOutside {
			t.Errorf("Resolve(%q) err = %v, want ErrOutside", rel, err)
		}
	}
}

func TestResolveReportsExpiryBeforeAskingForThePassword(t *testing.T) {
	past := time.Now().Add(-time.Hour).Unix()
	s, _ := newService(t, map[string]config.FileData{
		"flag":       {Path: "/srv", Expired: true},
		"date":       {Path: "/srv", Expiration: past},
		"lockedDate": {Path: "/srv", Expiration: past, Password: "hash"},
	})
	for _, sub := range []string{"flag", "date", "lockedDate"} {
		if _, err := s.Resolve(sub, "", Access{}); err != ErrExpired {
			t.Errorf("%s: err = %v, want ErrExpired", sub, err)
		}
	}
}

func TestResolveLockedUntilTokenOrDocumentServer(t *testing.T) {
	s, _ := newService(t, map[string]config.FileData{
		"priv":  {Path: "/srv/priv", Password: "hash"},
		"other": {Path: "/srv/other", Password: "hash"},
	})
	if _, err := s.Resolve("priv", "/a.docx", Access{}); err != ErrLocked {
		t.Fatalf("no token: err = %v, want ErrLocked", err)
	}

	token, err := s.grant("priv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve("priv", "/a.docx", Access{Token: token}); err != nil {
		t.Errorf("own token: %v", err)
	}
	if _, err := s.Resolve("other", "/a.docx", Access{Token: token}); err != ErrLocked {
		t.Errorf("token of another share: err = %v, want ErrLocked", err)
	}

	tok, _ := office.SignJWT(testSecret, map[string]any{
		"payload": map[string]any{"url": "https://fs.example.com/priv/a.docx?dl=1"},
		"exp":     time.Now().Add(time.Minute).Unix(),
	})
	ds := Access{AuthHeader: "Bearer " + tok, RequestURI: "/priv/a.docx?dl=1"}
	if _, err := s.Resolve("priv", "/a.docx", ds); err != nil {
		t.Errorf("document server: %v", err)
	}
	ds.RequestURI = "/priv/b.docx?dl=1"
	if _, err := s.Resolve("priv", "/b.docx", ds); err != ErrLocked {
		t.Errorf("document server token for another file: err = %v, want ErrLocked", err)
	}
}

func TestUploadTarget(t *testing.T) {
	hash, _ := password.Hash("pw")
	s, _ := newService(t, map[string]config.FileData{
		"up":     {Path: "/srv/up", AllowPost: true},
		"closed": {Path: "/srv/closed"},
		"old":    {Path: "/srv/old", AllowPost: true, Expired: true},
		"priv":   {Path: "/srv/priv", AllowPost: true, Password: hash},
	})
	if fd, err := s.UploadTarget("up", ""); err != nil || fd.Path != "/srv/up" {
		t.Errorf("up: %+v, %v", fd, err)
	}
	for sub, want := range map[string]error{"nope": ErrNotFound, "closed": ErrUploadsOff, "old": ErrExpired, "priv": ErrLocked} {
		if _, err := s.UploadTarget(sub, ""); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", sub, err, want)
		}
	}
	token, err := s.Unlock("priv", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadTarget("priv", token); err != nil {
		t.Errorf("unlocked password share: %v", err)
	}
}

func TestJoin(t *testing.T) {
	cases := map[string]bool{"": true, "/a/b": true, "/../x": false, "/../root2": false, "/a/../../x": false}
	for rel, want := range cases {
		if _, ok := Join("/root", rel); ok != want {
			t.Errorf("Join(/root, %q) ok = %v, want %v", rel, ok, want)
		}
	}
}
