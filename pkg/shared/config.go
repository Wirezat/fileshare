package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const defaultConfigPath = "./data.json"

// FileInfo holds the name, path, and type of a file or directory.
type FileInfo struct {
	Name  string
	Path  string
	IsDir bool
}

// FileData holds the sharing configuration for a single share.
type FileData struct {
	Path       string `json:"path"`
	UploadTime int64  `json:"upload_time"`
	Uses       int    `json:"uses"`
	Expiration int64  `json:"expiration"`
	Expired    bool   `json:"expired"`
	AllowPost  bool   `json:"allow_post"`
	Password   string `json:"password"`
}

// Config is the top-level application configuration.
type Config struct {
	Port          int                 `json:"port"`
	AdminUsername string              `json:"admin_username"`
	AdminPassword string              `json:"admin_password"`
	Files         map[string]FileData `json:"files"`
}

// LoadConfig loads the config from the default path.
func LoadConfig() (*Config, error) {
	return LoadConfigFrom(defaultConfigPath)
}

// LoadConfigFrom loads the config from the given path.
func LoadConfigFrom(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()

	var config Config
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	if config.Files == nil {
		config.Files = make(map[string]FileData)
	}
	return &config, nil
}

// SaveConfig writes the config atomically to the default path.
func SaveConfig(config *Config) error {
	return SaveConfigTo(defaultConfigPath, config)
}

// SaveConfigTo writes atomically: encodes to a temp file first, then renames.
// The temp file is created in the same directory as path so the rename is
// guaranteed to stay on the same filesystem (cross-device rename would fail).
func SaveConfigTo(path string, config *Config) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".data.json.tmp*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(config); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to encode config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	return nil
}
