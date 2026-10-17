package share

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
	"golang.org/x/crypto/bcrypt"
)

func lockedService(t *testing.T) *Service {
	t.Helper()
	hash, err := password.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newService(t, map[string]config.FileData{
		"priv":  {Path: "/srv/priv", Password: hash},
		"open":  {Path: "/srv/open"},
		"other": {Path: "/srv/other", Password: hash},
	})
	return s
}

func TestUnlockWithTheRightPassword(t *testing.T) {
	s := lockedService(t)
	token, err := s.Unlock("priv", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !s.TokenValid(token, "priv") || s.TokenValid(token, "other") {
		t.Error("token does not unlock exactly its own share")
	}
}

func TestUnlockRefusals(t *testing.T) {
	s := lockedService(t)
	cases := map[string]struct {
		subpath, pw string
		want        error
	}{
		"unknown share":  {"nope", "pw", ErrNotFound},
		"no password":    {"open", "pw", ErrNoPassword},
		"wrong password": {"priv", "falsch", ErrWrongPassword},
	}
	for name, tc := range cases {
		if token, err := s.Unlock(tc.subpath, tc.pw, "1.2.3.4"); !errors.Is(err, tc.want) || token != "" {
			t.Errorf("%s: token %q, err %v, want %v", name, token, err, tc.want)
		}
	}
}

func TestUnlockIsRateLimitedPerClientAndShare(t *testing.T) {
	s := lockedService(t)
	for range ratelimit.MaxFailures {
		s.Unlock("priv", "falsch", "1.2.3.4")
	}
	var limited RateLimitedError
	if _, err := s.Unlock("priv", "pw", "1.2.3.4"); !errors.As(err, &limited) || limited.RetryAfter <= 0 {
		t.Fatalf("right password while limited: err = %v, want RateLimitedError", err)
	}
	if _, err := s.Unlock("priv", "pw", "5.6.7.8"); err != nil {
		t.Errorf("other client limited too: %v", err)
	}
	if _, err := s.Unlock("other", "pw", "1.2.3.4"); err != nil {
		t.Errorf("other share limited too: %v", err)
	}
}

func TestUnlockUpgradesALegacyHash(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	s, st := newService(t, map[string]config.FileData{"old": {Path: "/srv", Password: string(legacy)}})
	token, err := s.Unlock("old", "pw", "1.2.3.4")
	if err != nil || !s.TokenValid(token, "old") {
		t.Fatalf("unlock: %v", err)
	}
	fd, _ := st.Current().Share("old")
	if !strings.HasPrefix(fd.Password, "$argon2id$") || !password.Check("pw", fd.Password) {
		t.Errorf("stored hash = %q, want an argon2id hash of the same password", fd.Password)
	}
}

func TestUpgradeKeepsAPasswordChangedMeanwhile(t *testing.T) {
	legacy, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	s, st := newService(t, map[string]config.FileData{"old": {Path: "/srv", Password: "changed-by-admin"}})
	s.upgradeHash("old", string(legacy), "pw")
	if fd, _ := st.Current().Share("old"); fd.Password != "changed-by-admin" {
		t.Errorf("password = %q, upgrade overwrote a newer password", fd.Password)
	}
}

func TestParallelWrongUnlocksReachThePasswordCheckAtMostMaxFailuresTimes(t *testing.T) {
	s := lockedService(t)
	var mu sync.Mutex
	checked := 0
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := s.Unlock("priv", "falsch", "1.2.3.4"); errors.Is(err, ErrWrongPassword) {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if checked != ratelimit.MaxFailures {
		t.Errorf("%d of 20 parallel unlocks reached the password check, want %d", checked, ratelimit.MaxFailures)
	}
}

func TestUpToMaxFailuresParallelRightUnlocksFromOneClientAllPass(t *testing.T) {
	s := lockedService(t)
	var mu sync.Mutex
	passed := 0
	var wg sync.WaitGroup
	for range ratelimit.MaxFailures {
		wg.Go(func() {
			if _, err := s.Unlock("priv", "pw", "1.2.3.4"); err == nil {
				mu.Lock()
				passed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if passed != ratelimit.MaxFailures {
		t.Errorf("%d of %d parallel right unlocks passed", passed, ratelimit.MaxFailures)
	}
}

func TestUnlockAfterFourTyposStillWorks(t *testing.T) {
	s := lockedService(t)
	for range ratelimit.MaxFailures - 1 {
		s.Unlock("priv", "falsch", "1.2.3.4")
	}
	if _, err := s.Unlock("priv", "pw", "1.2.3.4"); err != nil {
		t.Fatalf("err = %v, want a token", err)
	}
	for range ratelimit.MaxFailures - 1 {
		s.Unlock("priv", "falsch", "1.2.3.4")
	}
	if _, err := s.Unlock("priv", "pw", "1.2.3.4"); err != nil {
		t.Errorf("the right password did not clear the earlier typos: %v", err)
	}
}
