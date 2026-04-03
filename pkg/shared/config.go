package shared

import (
	"encoding/json"
	"fmt"
	"os"
)

const configFilePath = "./data.json"

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
	AdminPassword string              `json:"admin_password"`
	Files         map[string]FileData `json:"files"`
}

func LoadConfig() (*Config, error) {
	file, err := os.Open(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()

	var config Config
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	return &config, nil
}

// SaveConfig writes atomically: encodes to a temp file first, then renames.
// This prevents data.json from being corrupted if the process dies mid-write.
func SaveConfig(config *Config) error {
	tmp, err := os.CreateTemp(".", ".data.json.tmp*")
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

	if err := os.Rename(tmpName, configFilePath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	return nil
}
