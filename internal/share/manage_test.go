package share

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/store"
)

func ptr[T any](v T) *T { return &v }

func TestCreateCleansHashesAndStamps(t *testing.T) {
	s, st := newService(t, nil)
	dir := t.TempDir()
	before := time.Now().Unix()
	if err := s.Create("docs", config.FileData{Path: dir + "/", Password: "pw", Office: config.OfficeView}); err != nil {
		t.Fatal(err)
	}
	fd, ok := st.Current().Share("docs")
	if !ok || fd.Path != dir || fd.Office != config.OfficeView || fd.UploadTime < before {
		t.Errorf("stored = %+v", fd)
	}
	if !strings.HasPrefix(fd.Password, "$argon2id$") || !password.Check("pw", fd.Password) {
		t.Errorf("password stored as %q", fd.Password)
	}
}

func TestCreateRefusals(t *testing.T) {
	dir := t.TempDir()
	s, _ := newService(t, map[string]config.FileData{"docs": {Path: dir}})
	var invalid InvalidError
	for name, tc := range map[string]struct {
		subpath string
		fd      config.FileData
	}{
		"no subpath": {"", config.FileData{Path: dir}},
		"no path":    {"x", config.FileData{}},
		"bad office": {"x", config.FileData{Path: dir, Office: "print"}},
	} {
		if err := s.Create(tc.subpath, tc.fd); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want InvalidError", name, err)
		}
	}
	if err := s.Create("docs", config.FileData{Path: dir}); err != ErrExists {
		t.Errorf("duplicate: err = %v, want ErrExists", err)
	}
}

func TestPatchAppliesOnlySetFields(t *testing.T) {
	dir, next := t.TempDir(), t.TempDir()
	s, st := newService(t, map[string]config.FileData{"docs": {Path: dir, Password: "old", Office: config.OfficeEdit}})
	changes, err := s.Patch("docs", Patch{Path: ptr(next + "/"), AllowPost: ptr(true), Password: ptr("")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"path -> " + next, "allow_post -> true", "password -> <removed>"}
	if strings.Join(changes, "|") != strings.Join(want, "|") {
		t.Errorf("changes = %q, want %q", changes, want)
	}
	fd, _ := st.Current().Share("docs")
	if fd.Path != next || !fd.AllowPost || fd.Password != "" || fd.Office != config.OfficeEdit {
		t.Errorf("stored = %+v", fd)
	}
}

func TestPatchHashesANewPassword(t *testing.T) {
	s, st := newService(t, map[string]config.FileData{"docs": {Path: t.TempDir()}})
	changes, err := s.Patch("docs", Patch{Password: ptr("neu")})
	if err != nil || len(changes) != 1 || changes[0] != "password -> ***" {
		t.Fatalf("changes %q, err %v", changes, err)
	}
	if fd, _ := st.Current().Share("docs"); !password.Check("neu", fd.Password) {
		t.Errorf("stored password = %q", fd.Password)
	}
}

func TestPatchRefusals(t *testing.T) {
	s, _ := newService(t, map[string]config.FileData{"docs": {Path: t.TempDir()}})
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
		s, st := newService(t, map[string]config.FileData{"docs": {Path: t.TempDir()}})
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
	dir := t.TempDir()
	s, st := newService(t, map[string]config.FileData{"docs": {Path: dir}})
	fd, err := s.Delete("docs")
	if err != nil || fd.Path != dir {
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

type deleteBeforeUpdate struct {
	*store.FileStore
	subpath string
}

func (d deleteBeforeUpdate) Update(fn func(*config.Config) error) error {
	d.FileStore.Update(func(c *config.Config) error {
		delete(c.Files, d.subpath)
		return nil
	})
	return d.FileStore.Update(fn)
}

func TestPatchOfAShareDeletedMeanwhileDoesNotBringItBack(t *testing.T) {
	_, st := newService(t, map[string]config.FileData{"docs": {Path: t.TempDir()}})
	s := New(deleteBeforeUpdate{st, "docs"})
	if _, err := s.Patch("docs", Patch{AllowPost: ptr(true)}); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, ok := st.Current().Share("docs"); ok {
		t.Error("patch brought the deleted share back")
	}
}

func modeDir(t *testing.T, mode os.FileMode) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	dir := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	return dir
}

func wantInvalid(t *testing.T, err error, text string) {
	t.Helper()
	var invalid InvalidError
	if !errors.As(err, &invalid) || !strings.Contains(string(invalid), text) {
		t.Errorf("err = %v, want InvalidError containing %q", err, text)
	}
}

func TestCreateNeedsReadAndUploadWriteAccess(t *testing.T) {
	s, _ := newService(t, nil)
	wantInvalid(t, s.Create("a", config.FileData{Path: modeDir(t, 0o000)}), "not readable")
	wantInvalid(t, s.Create("b", config.FileData{Path: modeDir(t, 0o555), AllowPost: true}), "write access")
	if err := s.Create("c", config.FileData{Path: modeDir(t, 0o555)}); err != nil {
		t.Errorf("read-only share without uploads: %v", err)
	}
}

func TestPatchChecksPathAndAllowPost(t *testing.T) {
	s, st := newService(t, map[string]config.FileData{"docs": {Path: t.TempDir()}})
	_, err := s.Patch("docs", Patch{Path: ptr(modeDir(t, 0o000))})
	wantInvalid(t, err, "not readable")
	_, err = s.Patch("docs", Patch{Path: ptr(modeDir(t, 0o555)), AllowPost: ptr(true)})
	wantInvalid(t, err, "write access")
	ro := modeDir(t, 0o555)
	if _, err := s.Patch("docs", Patch{Path: &ro}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Patch("docs", Patch{AllowPost: ptr(true)})
	wantInvalid(t, err, "write access")
	if fd, _ := st.Current().Share("docs"); fd.Path != ro || fd.AllowPost {
		t.Errorf("refused patches changed the share: %+v", fd)
	}
}

func TestProblemsListsOnlyBrokenShares(t *testing.T) {
	s, _ := newService(t, map[string]config.FileData{
		"ok":     {Path: t.TempDir(), AllowPost: true},
		"locked": {Path: modeDir(t, 0o000)},
		"upload": {Path: modeDir(t, 0o555), AllowPost: true},
	})
	got := s.Problems()
	want := map[string]string{
		"locked": "path is not readable by the service user",
		"upload": "uploads need write access for the service user",
	}
	if !maps.Equal(got, want) {
		t.Errorf("Problems = %v, want %v", got, want)
	}
}
