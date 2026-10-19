package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func TestHandEditedDownloadLimitIsTakenOver(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	handEdit(t, path, `{"downloadLimitEnabled":true,"downloadLimitMbit":40,"files":{}}`)
	c.advance(checkInterval)
	if on, mbit := s.Current().DownloadLimit(); !on || mbit != 40 {
		t.Errorf("DownloadLimit() = %v, %d; want true, 40", on, mbit)
	}
}

func TestHandEditedInvalidDownloadLimitIsRefused(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"downloadLimitEnabled":true,"downloadLimitMbit":40,"files":{}}`)
	handEdit(t, path, `{"downloadLimitEnabled":true,"downloadLimitMbit":0,"files":{}}`)
	c.advance(checkInterval)
	if on, mbit := s.Current().DownloadLimit(); !on || mbit != 40 {
		t.Errorf("DownloadLimit() = %v, %d; want the last good 40", on, mbit)
	}
	if s.Refused() == "" {
		t.Error("invalid download limit not reported")
	}
}

func TestOpenRejectsAnInvalidDownloadLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(`{"downloadLimitMbit":-3,"files":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, config.ErrInvalidDownloadLimit) {
		t.Errorf("Open = %v, want ErrInvalidDownloadLimit", err)
	}
}

func TestUpdateNeverWritesAnInvalidConfig(t *testing.T) {
	s, path, _, _ := clockedStore(t, `{"files":{}}`)
	before, _ := os.ReadFile(path)
	err := s.Update(func(c *config.Config) error {
		c.DownloadLimitEnabled = true
		return nil
	})
	if !errors.Is(err, config.ErrInvalidDownloadLimit) {
		t.Fatalf("Update = %v, want ErrInvalidDownloadLimit", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("data.json changed")
	}
	if on, _ := s.Current().DownloadLimit(); on {
		t.Error("invalid update became current")
	}
}
