package ttl

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestGetReturnsValueUntilExpiry(t *testing.T) {
	s := New[string]()
	s.Set("live", "docs", time.Hour)
	s.Set("dead", "docs", -time.Second)
	if v, ok := s.Get("live"); !ok || v != "docs" {
		t.Errorf("Get(live) = %q, %v", v, ok)
	}
	if _, ok := s.Get("dead"); ok {
		t.Error("expired entry returned")
	}
	if _, ok := s.Get("missing"); ok {
		t.Error("missing entry returned")
	}
}

func TestDelete(t *testing.T) {
	s := New[int]()
	s.Set("a", 1, time.Hour)
	s.Delete("a")
	if _, ok := s.Get("a"); ok {
		t.Error("deleted entry returned")
	}
}

func TestDeleteExceptKeepsOnlyOne(t *testing.T) {
	s := New[int]()
	for _, k := range []string{"a", "b", "c"} {
		s.Set(k, 1, time.Hour)
	}
	if n := s.DeleteExcept("b"); n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if _, ok := s.Get("b"); !ok {
		t.Error("kept entry gone")
	}
	if _, ok := s.Get("a"); ok {
		t.Error("other entry kept")
	}
}

func TestSetSweepsExpiredEntriesOncePerInterval(t *testing.T) {
	s := New[int]()
	s.Set("live", 1, time.Hour)
	s.Set("dead", 1, -time.Second)
	s.Set("other", 1, time.Hour)
	if _, ok := s.entries["dead"]; !ok {
		t.Fatal("swept again before the interval passed")
	}
	s.swept = time.Now().Add(-sweepInterval - time.Second)
	s.Set("trigger", 1, time.Hour)
	if _, ok := s.entries["dead"]; ok {
		t.Error("expired entry survived the sweep")
	}
	if len(s.entries) != 3 {
		t.Errorf("entries = %d, want 3", len(s.entries))
	}
}

func TestTokenIsRandomHex(t *testing.T) {
	a, err := Token()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Token()
	if len(a) != 48 || a == b {
		t.Errorf("tokens %q, %q", a, b)
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Errorf("token not hex: %v", err)
	}
}

func TestDeleteFuncRemovesMatchingEntries(t *testing.T) {
	s := New[string]()
	s.Set("a", "docs", time.Hour)
	s.Set("b", "docs", time.Hour)
	s.Set("c", "other", time.Hour)
	if n := s.DeleteFunc(func(_ string, v string) bool { return v == "docs" }); n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if _, ok := s.Get("c"); !ok {
		t.Error("non-matching entry removed")
	}
	if _, ok := s.Get("a"); ok {
		t.Error("matching entry kept")
	}
}
