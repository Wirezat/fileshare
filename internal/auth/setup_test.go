package auth

import (
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/password"
	"github.com/Wirezat/fileshare/internal/ratelimit"
)

func TestSetupCodeOnlyWithoutAPassword(t *testing.T) {
	s, _ := newService(t, "admin", "")
	if code := s.SetupCode(); len(code) != 32 {
		t.Errorf("setup code = %q, want 32 hex chars", code)
	} else if _, err := hex.DecodeString(code); err != nil {
		t.Errorf("setup code %q is not hex", code)
	}
	if s, _ := adminService(t); s.SetupCode() != "" {
		t.Errorf("setup code = %q with a password set", s.SetupCode())
	}
}

func TestSetupStoresTheCredentialsOnce(t *testing.T) {
	s, st := newService(t, "admin", "")
	code := s.SetupCode()
	if err := s.Setup(code, "boss", "pw", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if st.Current().AdminUsername() != "boss" || !password.Check("pw", st.Current().AdminPassword()) {
		t.Errorf("credentials not stored: user %q", st.Current().AdminUsername())
	}
	if s.SetupCode() != "" || s.NeedsSetup() {
		t.Error("setup code survives setup")
	}
	if err := s.Setup(code, "evil", "x", "1.2.3.4"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second setup: err = %v, want ErrSetupDone", err)
	}
}

func TestSetupWithoutUserKeepsTheUsername(t *testing.T) {
	s, st := newService(t, "admin", "")
	if err := s.Setup(s.SetupCode(), "", "pw", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if got := st.Current().AdminUsername(); got != "admin" {
		t.Errorf("username = %q, want admin", got)
	}
}

func TestSetupRefusalsChangeNothing(t *testing.T) {
	s, st := newService(t, "admin", "")
	cases := map[string]struct {
		code, pass string
		want       error
	}{
		"wrong code":     {"falsch", "pw", ErrWrongSetupCode},
		"empty code":     {"", "pw", ErrWrongSetupCode},
		"empty password": {s.SetupCode(), "", ErrEmptyPassword},
	}
	for name, c := range cases {
		if err := s.Setup(c.code, "boss", c.pass, name); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if !s.NeedsSetup() || st.Current().AdminUsername() != "admin" {
		t.Error("a refused setup was stored")
	}
}

func TestSetupIsRateLimitedApartFromLogin(t *testing.T) {
	s, _ := newService(t, "admin", "")
	for range ratelimit.MaxFailures {
		s.Setup("falsch", "boss", "pw", "1.2.3.4")
	}
	var limited RateLimitedError
	if err := s.Setup(s.SetupCode(), "boss", "pw", "1.2.3.4"); !errors.As(err, &limited) {
		t.Fatalf("err = %v, want RateLimitedError", err)
	}
	if wait := s.limiter.Attempt("1.2.3.4"); wait != 0 {
		t.Errorf("login key blocked by setup failures for %s", wait)
	}
}

func TestSetupWithTheRightCodeClearsEarlierFailures(t *testing.T) {
	s, _ := newService(t, "admin", "")
	for range ratelimit.MaxFailures - 1 {
		s.Setup("falsch", "boss", "pw", "1.2.3.4")
	}
	if err := s.Setup(s.SetupCode(), "boss", "", "1.2.3.4"); !errors.Is(err, ErrEmptyPassword) {
		t.Fatalf("err = %v, want ErrEmptyPassword", err)
	}
	if err := s.Setup("falsch", "boss", "pw", "1.2.3.4"); !errors.Is(err, ErrWrongSetupCode) {
		t.Errorf("err = %v, want ErrWrongSetupCode: the right code did not clear the failures", err)
	}
}

func TestSetupLosesToAPasswordSetMeanwhile(t *testing.T) {
	s, st := newService(t, "admin", "")
	code := s.SetupCode()
	stale := &staleStore{FileStore: st}
	s.store = stale
	before := st.Current()
	st.Update(func(c *config.Config) error {
		c.AdminPassword = "set-elsewhere"
		return nil
	})
	stale.stale = &before
	if err := s.Setup(code, "boss", "pw", "1.2.3.4"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("err = %v, want ErrSetupDone", err)
	}
	if got := st.Current().AdminPassword(); got != "set-elsewhere" {
		t.Errorf("password = %q, setup overwrote it", got)
	}
}

func TestParallelWrongSetupsReachTheCodeCheckAtMostMaxFailuresTimes(t *testing.T) {
	s, _ := newService(t, "admin", "")
	var mu sync.Mutex
	checked := 0
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := s.Setup("falsch", "boss", "pw", "1.2.3.4"); errors.Is(err, ErrWrongSetupCode) {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if checked != ratelimit.MaxFailures {
		t.Errorf("%d of 20 parallel setups reached the code check, want %d", checked, ratelimit.MaxFailures)
	}
}

func TestSetupAfterFourTyposStillWorks(t *testing.T) {
	s, _ := newService(t, "admin", "")
	for range ratelimit.MaxFailures - 1 {
		s.Setup("falsch", "boss", "pw", "1.2.3.4")
	}
	if err := s.Setup(s.SetupCode(), "boss", "pw", "1.2.3.4"); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestParallelSetupsWithTheRightCodeSucceedOnce(t *testing.T) {
	s, _ := newService(t, "admin", "")
	code := s.SetupCode()
	var mu sync.Mutex
	succeeded := 0
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			err := s.Setup(code, "boss", "pw", string(rune('a'+i)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case !errors.Is(err, ErrSetupDone) && !errors.Is(err, ErrWrongSetupCode):
				t.Errorf("err = %v", err)
			}
		})
		wg.Go(func() { s.SetupCode() })
	}
	wg.Wait()
	if succeeded != 1 {
		t.Errorf("%d setups succeeded, want exactly 1", succeeded)
	}
}
