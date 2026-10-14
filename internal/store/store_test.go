package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func openTemp(t *testing.T, raw string) (*FileStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, path
}

func addShare(sub string) func(*config.Config) error {
	return func(c *config.Config) error {
		c.Files[sub] = config.FileData{Path: "/srv/" + sub}
		return nil
	}
}

func TestOpenAppliesDefaults(t *testing.T) {
	s, _ := openTemp(t, `{"admin_username":""}`)
	v := s.Current()
	if v.Port() != 27182 || v.AdminUsername() != "admin" || v.ShareCount() != 0 {
		t.Errorf("defaults not applied: port %d user %q", v.Port(), v.AdminUsername())
	}
}

func TestOpenFailsOnMissingOrBrokenFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing file: no error")
	}
	path := filepath.Join(t.TempDir(), "data.json")
	os.WriteFile(path, []byte("{"), 0o600)
	if _, err := Open(path); err == nil {
		t.Error("broken JSON: no error")
	}
}

func TestUpdateIsVisibleAndPersisted(t *testing.T) {
	s, path := openTemp(t, `{"files":{}}`)

	if err := s.Update(addShare("docs")); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Current().Share("docs"); !ok {
		t.Error("Current does not show the update")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Current().Share("docs"); !ok {
		t.Error("update was not written to disk")
	}
}

func TestFailedUpdateChangesNothing(t *testing.T) {
	s, path := openTemp(t, `{"files":{}}`)
	before, _ := os.ReadFile(path)

	err := s.Update(func(c *config.Config) error {
		c.Files["half"] = config.FileData{}
		return errors.New("refused")
	})
	if err == nil {
		t.Fatal("error was swallowed")
	}
	if _, ok := s.Current().Share("half"); ok {
		t.Error("Current shows a refused update")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("file changed on a refused update")
	}
}

func TestFailedSaveChangesNothing(t *testing.T) {
	s, path := openTemp(t, `{"files":{}}`)
	dir := filepath.Dir(path)
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := s.Update(addShare("docs")); err == nil {
		t.Skip("directory still writable (running as root?)")
	}
	if _, ok := s.Current().Share("docs"); ok {
		t.Error("Current shows an update that was never saved")
	}
}

func TestHeldViewDoesNotChange(t *testing.T) {
	s, _ := openTemp(t, `{"files":{}}`)
	held := s.Current()

	s.Update(addShare("docs"))

	if _, ok := held.Share("docs"); ok {
		t.Error("a view held before the update changed")
	}
}

func TestConcurrentUpdatesAreNotLost(t *testing.T) {
	s, path := openTemp(t, `{"files":{}}`)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() { s.Update(addShare(fmt.Sprintf("s%02d", i))) })
		wg.Go(func() {
			for range s.Current().Shares() {
			}
		})
	}
	wg.Wait()

	if n := s.Current().ShareCount(); n != 50 {
		t.Errorf("in memory: %d shares, want 50", n)
	}
	reopened, _ := Open(path)
	if n := reopened.Current().ShareCount(); n != 50 {
		t.Errorf("on disk: %d shares, want 50", n)
	}
}
