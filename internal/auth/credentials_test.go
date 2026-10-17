package auth

import (
	"errors"
	"sync"
	"testing"

	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
)

func sessions(t *testing.T, s *Service, n int) []string {
	t.Helper()
	var tokens []string
	for range n {
		token, err := s.newSession()
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func TestChangePasswordKeepsOnlyTheCallersSession(t *testing.T) {
	s, st := adminService(t)
	tokens := sessions(t, s, 3)
	dropped, err := s.ChangePassword("pw", "neu", tokens[0])
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 2 || !s.Valid(tokens[0]) || s.Valid(tokens[1]) || s.Valid(tokens[2]) {
		t.Errorf("dropped %d, sessions %v %v %v", dropped, s.Valid(tokens[0]), s.Valid(tokens[1]), s.Valid(tokens[2]))
	}
	if !password.Check("neu", st.Current().AdminPassword()) {
		t.Error("new password not stored")
	}
}

func TestChangeUsernameKeepsOnlyTheCallersSession(t *testing.T) {
	s, st := adminService(t)
	tokens := sessions(t, s, 2)
	dropped, err := s.ChangeUsername("pw", "boss", tokens[1])
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 || s.Valid(tokens[0]) || !s.Valid(tokens[1]) {
		t.Errorf("dropped %d, sessions %v %v", dropped, s.Valid(tokens[0]), s.Valid(tokens[1]))
	}
	if got := st.Current().AdminUsername(); got != "boss" {
		t.Errorf("username = %q, want boss", got)
	}
}

func TestCredentialChangeRefusalsChangeNothing(t *testing.T) {
	s, st := adminService(t)
	tokens := sessions(t, s, 2)
	before := st.Current().AdminPassword()
	cases := map[string]struct {
		err  error
		want error
	}{
		"username, wrong current": {second(s.ChangeUsername("falsch", "boss", tokens[0])), ErrWrongPassword},
		"username, empty":         {second(s.ChangeUsername("pw", "", tokens[0])), ErrEmptyUsername},
		"password, wrong current": {second(s.ChangePassword("falsch", "neu", tokens[0])), ErrWrongPassword},
		"password, empty":         {second(s.ChangePassword("pw", "", tokens[0])), ErrEmptyPassword},
	}
	for name, c := range cases {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, c.err, c.want)
		}
	}
	if st.Current().AdminUsername() != "admin" || st.Current().AdminPassword() != before {
		t.Error("a refused change was stored")
	}
	if !s.Valid(tokens[0]) || !s.Valid(tokens[1]) {
		t.Error("a refused change ended sessions")
	}
}

func second(_ int, err error) error { return err }

func TestCredentialChangeIsRateLimitedPerSession(t *testing.T) {
	s, _ := adminService(t)
	tokens := sessions(t, s, 2)
	for range ratelimit.MaxFailures {
		if _, err := s.ChangePassword("falsch", "neu", tokens[0]); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("err = %v, want ErrWrongPassword", err)
		}
	}
	var limited RateLimitedError
	if _, err := s.ChangePassword("pw", "neu", tokens[0]); !errors.As(err, &limited) {
		t.Errorf("password, 6th try: err = %v, want RateLimitedError", err)
	}
	if _, err := s.ChangeUsername("pw", "boss", tokens[0]); !errors.As(err, &limited) {
		t.Errorf("username, same session: err = %v, want RateLimitedError", err)
	}
	if _, err := s.ChangeUsername("pw", "boss", tokens[1]); err != nil {
		t.Errorf("other session blocked: %v", err)
	}
}

func TestParallelWrongCredentialChangesReachThePasswordCheckAtMostMaxFailuresTimes(t *testing.T) {
	s, _ := adminService(t)
	token := sessions(t, s, 1)[0]
	var mu sync.Mutex
	checked := 0
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := s.ChangePassword("falsch", "neu", token); errors.Is(err, ErrWrongPassword) {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if checked != ratelimit.MaxFailures {
		t.Errorf("%d of 20 parallel changes reached the password check, want %d", checked, ratelimit.MaxFailures)
	}
}

func TestCredentialChangeAfterFourTyposStillWorks(t *testing.T) {
	s, _ := adminService(t)
	token := sessions(t, s, 1)[0]
	for range ratelimit.MaxFailures - 1 {
		s.ChangePassword("falsch", "neu", token)
	}
	if _, err := s.ChangeUsername("pw", "boss", token); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	for range ratelimit.MaxFailures - 1 {
		s.ChangeUsername("falsch", "chef", token)
	}
	if _, err := s.ChangePassword("pw", "neu", token); err != nil {
		t.Errorf("the successful change did not clear the earlier typos: %v", err)
	}
}
