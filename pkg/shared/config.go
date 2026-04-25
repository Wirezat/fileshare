package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"sync/atomic"
)

const defaultConfigPath = "./data.json"

var configDefaults = Config{
	Port:                   27182,
	MaxPostSize:            94371840,
	ChunkInactivityTimeout: 1800,
	AdminUsername:          "admin",
	AdminPassword:          "admin",
}

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
	Port                   int                 `json:"port"`
	MaxPostSize            int                 `json:"maxPostSize"`
	ChunkInactivityTimeout int                 `json:"chunkInactivityTimeout"`
	AdminUsername          string              `json:"admin_username"`
	AdminPassword          string              `json:"admin_password"`
	Files                  map[string]FileData `json:"files"`
}

var configCache atomic.Pointer[Config]

// LoadConfig loads the config from the default path.
func LoadConfig() (*Config, error) {
	if p := configCache.Load(); p != nil {
		return p, nil
	}
	cfg, err := LoadConfigFrom(defaultConfigPath)
	if err != nil {
		return nil, err
	}
	configCache.Store(cfg)
	return cfg, nil
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
	applyDefaults(&config)
	return &config, nil
}

// applyDefaults fills in zero-value fields that would break the server if left unset.
func applyDefaults(cfg *Config) {
	defaults := reflect.ValueOf(configDefaults)
	target := reflect.ValueOf(cfg).Elem()

	for i := range target.NumField() {
		f := target.Field(i)
		if f.IsZero() {
			f.Set(defaults.Field(i))
		}
	}
}

// SaveConfig writes the config atomically to the default path.
func SaveConfig(cfg *Config) error {
	if err := SaveConfigTo(defaultConfigPath, cfg); err != nil {
		return err
	}
	configCache.Store(cfg)
	return nil
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
