package password

import (
	"strings"
	"testing"

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
