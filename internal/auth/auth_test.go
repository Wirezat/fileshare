package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
	"github.com/Wirezat/fileshare/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func newService(t *testing.T, user, hash string) (*Service, *store.FileStore) {
	t.Helper()
	data, err := json.Marshal(config.Config{AdminUsername: user, AdminPassword: hash})
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

func adminService(t *testing.T) (*Service, *store.FileStore) {
	t.Helper()
	hash, err := password.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	return newService(t, "admin", hash)
}

func TestLoginStartsASessionUntilLogout(t *testing.T) {
	s, _ := adminService(t)
	token, err := s.Login("admin", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Valid(token) {
		t.Fatal("fresh session is not valid")
	}
	s.Logout(token)
	if s.Valid(token) {
		t.Error("session still valid after logout")
	}
	if s.Valid("forged") || s.Valid("") {
		t.Error("unknown token is valid")
	}
}

func TestLoginRefusals(t *testing.T) {
	s, _ := adminService(t)
	for name, c := range map[string][2]string{
		"wrong user":     {"other", "pw"},
		"wrong password": {"admin", "falsch"},
		"both empty":     {"", ""},
	} {
		if token, err := s.Login(c[0], c[1], name); !errors.Is(err, ErrWrongCredentials) || token != "" {
			t.Errorf("%s: token %q, err %v, want ErrWrongCredentials", name, token, err)
		}
	}
}

func TestLoginBeforeSetup(t *testing.T) {
	s, _ := newService(t, "admin", "")
	if !s.NeedsSetup() {
		t.Error("NeedsSetup = false without a password")
	}
	if _, err := s.Login("admin", "", "1.2.3.4"); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("err = %v, want ErrNotSetUp", err)
	}
}

func TestLoginIsRateLimitedPerClient(t *testing.T) {
	s, _ := adminService(t)
	for range ratelimit.MaxFailures {
		s.Login("admin", "falsch", "1.2.3.4")
	}
	var limited RateLimitedError
	if _, err := s.Login("admin", "pw", "1.2.3.4"); !errors.As(err, &limited) || limited.RetryAfter <= 0 {
		t.Errorf("err = %v, want RateLimitedError with a wait", err)
	}
	if _, err := s.Login("admin", "pw", "5.6.7.8"); err != nil {
		t.Errorf("other client: err = %v, want nil", err)
	}
}

func TestLoginUpgradesALegacyHash(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	s, st := newService(t, "admin", string(legacy))
	if _, err := s.Login("admin", "pw", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	stored := st.Current().AdminPassword()
	if password.NeedsRehash(stored) || !password.Check("pw", stored) {
		t.Errorf("hash not upgraded: %q", stored)
	}
}

func TestUpgradeKeepsAPasswordChangedMeanwhile(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	s, st := newService(t, "admin", "changed-by-admin")
	if got := s.upgradeHash(string(legacy), "pw"); got != string(legacy) {
		t.Errorf("upgradeHash = %q, want the old hash back", got)
	}
	if got := st.Current().AdminPassword(); got != "changed-by-admin" {
		t.Errorf("password = %q, upgrade overwrote a newer password", got)
	}
}

func TestLoginAfterFourTyposStillGetsIn(t *testing.T) {
	s, _ := adminService(t)
	for range ratelimit.MaxFailures - 1 {
		s.Login("admin", "falsch", "1.2.3.4")
	}
	if _, err := s.Login("admin", "pw", "1.2.3.4"); err != nil {
		t.Fatalf("err = %v, want a session", err)
	}
	for range ratelimit.MaxFailures - 1 {
		s.Login("admin", "falsch", "1.2.3.4")
	}
	if _, err := s.Login("admin", "pw", "1.2.3.4"); err != nil {
		t.Errorf("the successful login did not clear the earlier typos: %v", err)
	}
}
