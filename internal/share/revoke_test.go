package share

import (
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func unlocked(t *testing.T) (*Service, string, string) {
	t.Helper()
	s := lockedService(t)
	priv, err := s.Unlock("priv", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Unlock("other", "pw", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	return s, priv, other
}

func TestPasswordChangeEndsExistingAccess(t *testing.T) {
	for name, pw := range map[string]string{"new password": "neu", "password removed": ""} {
		s, priv, other := unlocked(t)
		if _, err := s.Patch("priv", Patch{Password: ptr(pw)}); err != nil {
			t.Fatal(err)
		}
		if s.TokenValid(priv, "priv") {
			t.Errorf("%s: old token still opens the share", name)
		}
		if !s.TokenValid(other, "other") {
			t.Errorf("%s: token of another share revoked", name)
		}
	}
}

func TestOtherPatchesKeepAccess(t *testing.T) {
	s, priv, _ := unlocked(t)
	if _, err := s.Patch("priv", Patch{NoZip: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if !s.TokenValid(priv, "priv") {
		t.Error("patch without password change revoked the token")
	}
}

func TestDeleteEndsAccessForARecreatedShare(t *testing.T) {
	s, priv, other := unlocked(t)
	if _, err := s.Delete("priv"); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("priv", config.FileData{Path: t.TempDir(), Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	if s.TokenValid(priv, "priv") {
		t.Error("token from before the delete opens the recreated share")
	}
	if !s.TokenValid(other, "other") {
		t.Error("token of another share revoked")
	}
}
