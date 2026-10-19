package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func editableService(t *testing.T) (*Service, *store.FileStore, string) {
	t.Helper()
	hash, err := password.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(config.Config{AdminUsername: "admin", AdminPassword: hash})
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

func editByHand(t *testing.T, st *store.FileStore, path string, change func(*config.Config)) {
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
	if err := st.Update(func(*config.Config) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestHandEditedCredentialsEndEverySession(t *testing.T) {
	newHash, _ := password.Hash("neu")
	for name, change := range map[string]func(*config.Config){
		"password": func(c *config.Config) { c.AdminPassword = newHash },
		"username": func(c *config.Config) { c.AdminUsername = "boss" },
	} {
		t.Run(name, func(t *testing.T) {
			s, st, path := editableService(t)
			tokens := sessions(t, s, 2)
			editByHand(t, st, path, change)
			if s.Valid(tokens[0]) || s.Valid(tokens[1]) {
				t.Error("a session survived a hand edit of the admin credentials")
			}
		})
	}
}

func TestHandEditOfSomethingElseKeepsSessions(t *testing.T) {
	s, st, path := editableService(t)
	token := sessions(t, s, 1)[0]
	editByHand(t, st, path, func(c *config.Config) { c.MaxPostSize = 1234 })
	if !s.Valid(token) {
		t.Error("an unrelated hand edit ended a session")
	}
}

func TestHandClearedPasswordOpensSetup(t *testing.T) {
	s, st, path := editableService(t)
	if s.SetupCode() != "" {
		t.Fatal("setup code before the password was cleared")
	}
	editByHand(t, st, path, func(c *config.Config) { c.AdminPassword = "" })

	code := s.SetupCode()
	if code == "" {
		t.Fatal("no setup code after the password was cleared by hand")
	}
	if err := s.Setup(code, "", "neu", "1.2.3.4"); err != nil {
		t.Errorf("setup with the new code: %v", err)
	}
}

func TestLoginWithALegacyHashLeavesARefusedFileAlone(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	data, _ := json.Marshal(config.Config{AdminUsername: "admin", AdminPassword: string(legacy)})
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

	if _, err := s.Login("admin", "pw", "1.2.3.4"); err != nil {
		t.Fatalf("login while data.json is refused: %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"files":` {
		t.Errorf("data.json = %q, want the refused file untouched", raw)
	}
	if aside, _ := filepath.Glob(path + ".rejected-*"); len(aside) != 0 {
		t.Errorf("set-aside files = %v, want none", aside)
	}
	if st.Refused() == "" {
		t.Error("refusal cleared by a login")
	}
}
