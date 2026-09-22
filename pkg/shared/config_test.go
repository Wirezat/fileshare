package shared

import (
	"os"
	"testing"
)

func TestLoadConfigAlwaysReadsFromDisk(t *testing.T) {
	t.Cleanup(func() { os.Remove(defaultConfigPath) })

	if err := SaveConfig(&Config{AdminUsername: "admin", Files: map[string]FileData{}}); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	if cfg, _ := LoadConfig(); len(cfg.Files) != 0 {
		t.Fatalf("test setup broken: unexpected files %+v", cfg.Files)
	}

	// Simulate a second process (the CLI) writing the file directly.
	external := &Config{AdminUsername: "admin", Files: map[string]FileData{"docs": {Path: "/tmp"}}}
	if err := SaveConfigTo(defaultConfigPath, external); err != nil {
		t.Fatalf("external save: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Files["docs"].Path != "/tmp" {
		t.Errorf("LoadConfig did not see the external change: %+v", cfg.Files)
	}
}
