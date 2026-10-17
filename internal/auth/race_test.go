package auth

import (
	"errors"
	"sync"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
	"github.com/Wirezat/fileshare/internal/store"
)

type staleStore struct {
	*store.FileStore
	stale *config.View
}

func (s *staleStore) Current() config.View {
	if v := s.stale; v != nil {
		s.stale = nil
		return *v
	}
	return s.FileStore.Current()
}

func loginDuringChange(t *testing.T, change func(*config.Config)) {
	t.Helper()
	_, st := adminService(t)
	before := st.Current()
	if err := st.Update(func(c *config.Config) error { change(c); return nil }); err != nil {
		t.Fatal(err)
	}
	stale := &staleStore{FileStore: st}
	s := New(stale)
	stale.stale = &before
	token, err := s.Login("admin", "pw", "1.2.3.4")
	if !errors.Is(err, ErrWrongCredentials) || token != "" || s.Valid(token) {
		t.Errorf("token %q, err %v: a login checked against the old credentials kept a session", token, err)
	}
}

func TestLoginRacingAPasswordChangeKeepsNoSession(t *testing.T) {
	hash, _ := password.Hash("neu")
	loginDuringChange(t, func(c *config.Config) { c.AdminPassword = hash })
}

func TestLoginRacingAUsernameChangeKeepsNoSession(t *testing.T) {
	loginDuringChange(t, func(c *config.Config) { c.AdminUsername = "boss" })
}

func TestParallelWrongLoginsReachThePasswordCheckAtMostMaxFailuresTimes(t *testing.T) {
	s, _ := adminService(t)
	var mu sync.Mutex
	checked := 0
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := s.Login("admin", "falsch", "1.2.3.4"); errors.Is(err, ErrWrongCredentials) {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if checked != ratelimit.MaxFailures {
		t.Errorf("%d of 20 parallel logins reached the password check, want %d", checked, ratelimit.MaxFailures)
	}
}

type loginDuringUpdate struct {
	*store.FileStore
	s     *Service
	token string
}

func (h *loginDuringUpdate) Update(fn func(*config.Config) error) error {
	if h.s != nil {
		s := h.s
		h.s = nil
		h.token, _ = s.Login("admin", "pw", "other")
	}
	return h.FileStore.Update(fn)
}

func changeDuringLogin(t *testing.T, change func(s *Service, keep string) (int, error)) {
	t.Helper()
	_, st := adminService(t)
	hook := &loginDuringUpdate{FileStore: st}
	s := New(hook)
	keep, err := s.Login("admin", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	hook.s = s
	if _, err := change(s, keep); err != nil {
		t.Fatal(err)
	}
	if hook.token == "" {
		t.Fatal("the login during the change did not get a session")
	}
	if s.Valid(hook.token) {
		t.Error("a login that finished while the credentials were being written kept its session")
	}
	if !s.Valid(keep) {
		t.Error("the caller's own session was ended")
	}
}

func TestPasswordChangeEndsASessionStartedWhileItWrites(t *testing.T) {
	changeDuringLogin(t, func(s *Service, keep string) (int, error) { return s.ChangePassword("pw", "neu", keep) })
}

func TestUsernameChangeEndsASessionStartedWhileItWrites(t *testing.T) {
	changeDuringLogin(t, func(s *Service, keep string) (int, error) { return s.ChangeUsername("pw", "boss", keep) })
}
