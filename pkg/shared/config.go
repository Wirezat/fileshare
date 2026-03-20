package shared

import (
	"encoding/json"
	"fmt"
	"os"
)

// #region constants

const (
	configFilePath = "./data.json"
)

// #endregion

// #region structs

// FileInfo contains the name, path and type of a file or directory
type FileInfo struct {
	Name  string
	Path  string
	IsDir bool
}

// FileData contains the sharing configuration for a single share
type FileData struct {
	Path       string `json:"path"`
	UploadTime int64  `json:"upload_time"`
	Uses       int    `json:"uses"`
	Expiration int64  `json:"expiration"`
	Expired    bool   `json:"expired"`
	AllowPost  bool   `json:"allow_post"`
	Password   string `json:"password"`
}

// JsonData is the top-level configuration structure
type JsonData struct {
	Port          int                 `json:"port"`
	AdminPassword string              `json:"admin_password"`
	Files         map[string]FileData `json:"files"`
}

// #endregion

// #region config I/O

func LoadConfig() (JsonData, error) {
	file, err := os.Open(configFilePath)
	if err != nil {
		return JsonData{}, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var config JsonData
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return JsonData{}, fmt.Errorf("failed to decode config: %w", err)
	}
	return config, nil
}

func SaveConfig(config JsonData) error {
	file, err := os.Create(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to open config file for writing: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return nil
}

// #endregion
