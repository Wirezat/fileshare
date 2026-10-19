package share

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func editByHand(t *testing.T, st *store.FileStore, path string, change func(*config.Config)) {
	t.Helper()
	writeByHand(t, path, change)
	if err := st.Update(func(*config.Config) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func writeByHand(t *testing.T, path string, change func(*config.Config)) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c config.Config
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	change(&c)
	raw, _ = json.Marshal(c)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	os.Chtimes(path, later, later)
}

func TestHandEditedShareEndsItsUnlocks(t *testing.T) {
	newHash, _ := password.Hash("neu")
	for name, change := range map[string]func(*config.Config){
		"password": func(c *config.Config) { fd := c.Files["priv"]; fd.Password = newHash; c.Files["priv"] = fd },
		"path":     func(c *config.Config) { fd := c.Files["priv"]; fd.Path = "/srv/elsewhere"; c.Files["priv"] = fd },
		"removed":  func(c *config.Config) { delete(c.Files, "priv") },
	} {
		t.Run(name, func(t *testing.T) {
			s, st, path := lockedServiceAt(t)
			token, err := s.Unlock("priv", "pw", "1.2.3.4")
			if err != nil {
				t.Fatal(err)
			}
			other, err := s.Unlock("other", "pw", "1.2.3.4")
			if err != nil {
				t.Fatal(err)
			}
			editByHand(t, st, path, change)
			if s.TokenValid(token, "priv") {
				t.Error("unlock survived a hand edit of its share")
			}
			if !s.TokenValid(other, "other") {
				t.Error("unlock of an untouched share ended")
			}
		})
	}
}

func lockedServiceAt(t *testing.T) (*Service, *store.FileStore, string) {
	t.Helper()
	hash, err := password.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(config.Config{AdminUsername: "admin", Files: map[string]config.FileData{
		"priv":  {Path: "/srv/priv", Password: hash},
		"other": {Path: "/srv/other", Password: hash},
	}})
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return New(st), st, path
}

func TestRevokingThroughTheAdminWorksWhileTheFileIsBroken(t *testing.T) {
	s, _, path := lockedServiceAt(t)
	token, err := s.Unlock("priv", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"files":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Delete("priv"); err != nil {
		t.Fatalf("deleting a share while data.json is broken: %v", err)
	}
	if s.TokenValid(token, "priv") {
		t.Error("unlock survived the deletion")
	}
}

func TestVisitorUnlockWithALegacyHashLeavesARefusedFileAlone(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	data, _ := json.Marshal(config.Config{Files: map[string]config.FileData{"priv": {Path: "/srv/priv", Password: string(legacy)}}})
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(st)
	if err := os.WriteFile(path, []byte(`{"files":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Unlock("priv", "pw", "1.2.3.4"); err != nil {
		t.Fatalf("unlock while data.json is refused: %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"files":` {
		t.Errorf("data.json = %q, want the refused file untouched", raw)
	}
	if aside, _ := filepath.Glob(path + ".rejected-*"); len(aside) != 0 {
		t.Errorf("set-aside files = %v, want none", aside)
	}
	if st.Refused() == "" {
		t.Error("refusal cleared by a visitor's unlock")
	}
}

type reloadAfterFirstRead struct {
	*store.FileStore
	reads int
	edit  func()
}

func (r *reloadAfterFirstRead) Current() config.View {
	v := r.FileStore.Current()
	if r.reads++; r.reads == 1 {
		r.edit()
	}
	return v
}

func TestUnlockWithThePasswordAHandEditJustReplacedFails(t *testing.T) {
	_, st, path := lockedServiceAt(t)
	newHash, _ := password.Hash("neu")
	r := &reloadAfterFirstRead{FileStore: st}
	r.edit = func() {
		writeByHand(t, path, func(c *config.Config) { fd := c.Files["priv"]; fd.Password = newHash; c.Files["priv"] = fd })
		if err := st.Update(func(*config.Config) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	s := New(r)

	token, err := s.Unlock("priv", "pw", "1.2.3.4")
	if err == nil || s.TokenValid(token, "priv") {
		t.Errorf("Unlock with the replaced password = %v, token valid %v; want refused", err, s.TokenValid(token, "priv"))
	}
}

func TestLegacyUnlockAfterAHandEditOfThePasswordFailsAndUpgradesNothing(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	data, _ := json.Marshal(config.Config{Files: map[string]config.FileData{"priv": {Path: "/srv/priv", Password: string(legacy)}}})
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(st)
	var mu sync.Mutex
	var logs []string
	prev := GoLog.OnRecord
	GoLog.OnRecord = func(r GoLog.Record) { mu.Lock(); logs = append(logs, r.Message); mu.Unlock() }
	t.Cleanup(func() { GoLog.OnRecord = prev })
	newHash, _ := password.Hash("neu")
	writeByHand(t, path, func(c *config.Config) { fd := c.Files["priv"]; fd.Password = newHash; c.Files["priv"] = fd })

	token, err := s.Unlock("priv", "pw", "1.2.3.4")
	if err == nil || s.TokenValid(token, "priv") {
		t.Errorf("Unlock with the old password = %v; want refused", err)
	}
	if got, _ := st.Current().Share("priv"); got.Password != newHash {
		t.Error("hand-edited password was replaced")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, l := range logs {
		if strings.Contains(l, "upgraded") {
			t.Errorf("log %q claims an upgrade that did not happen", l)
		}
	}
}
