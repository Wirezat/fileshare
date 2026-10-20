package password

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHashThenCheck(t *testing.T) {
	h, err := Hash("geheim")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=2,p=4$") {
		t.Errorf("hash = %q", h)
	}
	if !Check("geheim", h) {
		t.Error("correct password rejected")
	}
	if Check("falsch", h) {
		t.Error("wrong password accepted")
	}
}

func TestHashSaltsEveryCall(t *testing.T) {
	a, _ := Hash("x")
	b, _ := Hash("x")
	if a == b {
		t.Error("two hashes of the same password are equal")
	}
}

func TestLegacyBcryptChecksAndNeedsRehash(t *testing.T) {
	legacy, err := bcrypt.GenerateFromPassword([]byte("alt"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if !Check("alt", string(legacy)) {
		t.Error("bcrypt hash rejected")
	}
	if !NeedsRehash(string(legacy)) {
		t.Error("bcrypt hash does not need rehash")
	}
}

func TestNeedsRehashFalseForArgonAndEmpty(t *testing.T) {
	h, _ := Hash("x")
	if NeedsRehash(h) || NeedsRehash("") {
		t.Error("NeedsRehash true for argon2id or empty hash")
	}
}

func TestCheckRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{
		"",
		"plain",
		"$argon2id$",
		"$argon2id$v=19$m=1,t=1,p=1$!!$!!",
		"$argon2id$v=18$m=65536,t=2,p=4$c2FsdHNhbHQ$aGFzaA",
	} {
		if Check("x", h) {
			t.Errorf("Check accepted %q", h)
		}
	}
}

func TestConcurrentChecksStayWithinTheLimit(t *testing.T) {
	h, err := Hash("geheim")
	if err != nil {
		t.Fatal(err)
	}
	var running, peak atomic.Int32
	orig := argonKey
	argonKey = func(pw, salt []byte, tm, memory uint32, threads uint8, keyLen uint32) []byte {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return orig(pw, salt, 1, 8, 1, keyLen)
	}
	t.Cleanup(func() { argonKey = orig })

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Check("falsch", h)
		}()
	}
	wg.Wait()
	if got, limit := peak.Load(), int32(cap(argonSlots)); got > limit || got < 1 {
		t.Errorf("peak concurrent checks = %d, want 1..%d", got, limit)
	}
}
