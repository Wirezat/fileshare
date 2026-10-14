package share

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
)

func ptr[T any](v T) *T { return &v }

func TestCreateCleansHashesAndStamps(t *testing.T) {
	s, st := newService(t, nil)
	before := time.Now().Unix()
	if err := s.Create("docs", config.FileData{Path: "/srv/docs/", Password: "pw", Office: config.OfficeView}); err != nil {
		t.Fatal(err)
	}
	fd, ok := st.Current().Share("docs")
	if !ok || fd.Path != "/srv/docs" || fd.Office != config.OfficeView || fd.UploadTime < before {
		t.Errorf("stored = %+v", fd)
	}
	if !strings.HasPrefix(fd.Password, "$argon2id$") || !password.Check("pw", fd.Password) {
		t.Errorf("password stored as %q", fd.Password)
	}
}

func TestCreateRefusals(t *testing.T) {
	s, _ := newService(t, map[string]config.FileData{"docs": {Path: "/srv"}})
	var invalid InvalidError
	for name, tc := range map[string]struct {
		subpath string
		fd      config.FileData
	}{
		"no subpath": {"", config.FileData{Path: "/srv"}},
		"no path":    {"x", config.FileData{}},
		"bad office": {"x", config.FileData{Path: "/srv", Office: "print"}},
	} {
		if err := s.Create(tc.subpath, tc.fd); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want InvalidError", name, err)
		}
	}
	if err := s.Create("docs", config.FileData{Path: "/other"}); err != ErrExists {
		t.Errorf("duplicate: err = %v, want ErrExists", err)
	}
}

func TestPatchAppliesOnlySetFields(t *testing.T) {
	s, st := newService(t, map[string]config.FileData{"docs": {Path: "/srv", Password: "old", Office: config.OfficeEdit}})
	changes, err := s.Patch("docs", Patch{Path: ptr("/srv/new/"), AllowPost: ptr(true), Password: ptr("")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"path -> /srv/new", "allow_post -> true", "password -> <removed>"}
	if strings.Join(changes, "|") != strings.Join(want, "|") {
		t.Errorf("changes = %q, want %q", changes, want)
	}
	fd, _ := st.Current().Share("docs")
	if fd.Path != "/srv/new" || !fd.AllowPost || fd.Password != "" || fd.Office != config.OfficeEdit {
		t.Errorf("stored = %+v", fd)
	}
}

func TestPatchHashesANewPassword(t *testing.T) {
	s, st := newService(t, map[string]config.FileData{"docs": {Path: "/srv"}})
	changes, err := s.Patch("docs", Patch{Password: ptr("neu")})
	if err != nil || len(changes) != 1 || changes[0] != "password -> ***" {
		t.Fatalf("changes %q, err %v", changes, err)
	}
	if fd, _ := st.Current().Share("docs"); !password.Check("neu", fd.Password) {
		t.Errorf("stored password = %q", fd.Password)
	}
}

func TestPatchRefusals(t *testing.T) {
	s, _ := newService(t, map[string]config.FileData{"docs": {Path: "/srv"}})
	if _, err := s.Patch("nope", Patch{AllowPost: ptr(true)}); err != ErrNotFound {
		t.Errorf("unknown share: err = %v", err)
	}
	var invalid InvalidError
	for name, p := range map[string]Patch{
		"empty path": {Path: ptr("")},
		"bad office": {Office: ptr("print")},
		"nothing":    {},
	} {
		if _, err := s.Patch("docs", p); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want InvalidError", name, err)
		}
	}
}

func TestConcurrentPatchesOfDifferentFieldsBothSurvive(t *testing.T) {
	for range 50 {
		s, st := newService(t, map[string]config.FileData{"docs": {Path: "/srv"}})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); s.Patch("docs", Patch{AllowPost: ptr(true)}) }()
		go func() { defer wg.Done(); s.Patch("docs", Patch{NoZip: ptr(true)}) }()
		wg.Wait()
		if fd, _ := st.Current().Share("docs"); !fd.AllowPost || !fd.NoZip {
			t.Fatalf("one patch lost: %+v", fd)
		}
	}
}

func TestDeleteReturnsTheRemovedShare(t *testing.T) {
	s, st := newService(t, map[string]config.FileData{"docs": {Path: "/srv"}})
	fd, err := s.Delete("docs")
	if err != nil || fd.Path != "/srv" {
		t.Fatalf("deleted %+v, err %v", fd, err)
	}
	if _, ok := st.Current().Share("docs"); ok {
		t.Error("share still there")
	}
	if fd, err := s.Delete("nope"); err != nil || fd.Path != "" {
		t.Errorf("unknown share: %+v, %v", fd, err)
	}
}

func TestPruneRemovesOnlyExpiredShares(t *testing.T) {
	s, st := newService(t, map[string]config.FileData{
		"flag": {Path: "/a", Expired: true},
		"date": {Path: "/b", Expiration: time.Now().Add(-time.Hour).Unix()},
		"live": {Path: "/c", Expiration: time.Now().Add(time.Hour).Unix()},
		"open": {Path: "/d"},
	})
	n, err := s.Prune()
	if err != nil || n != 2 {
		t.Fatalf("pruned %d, err %v", n, err)
	}
	if st.Current().ShareCount() != 2 {
		t.Errorf("left %d shares, want 2", st.Current().ShareCount())
	}
}
