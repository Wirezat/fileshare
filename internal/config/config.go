package config

import (
	"maps"
	"reflect"
	"time"
)

var defaults = Config{
	Port:                   27182,
	MaxPostSize:            94371840,
	ChunkInactivityTimeout: 1800,
	AdminUsername:          "admin",
}

const (
	OfficeOff  = ""
	OfficeView = "view"
	OfficeEdit = "edit"
)

// ValidOffice reports whether mode is one of the office modes.
func ValidOffice(mode string) bool {
	return mode == OfficeOff || mode == OfficeView || mode == OfficeEdit
}

// FileData holds the sharing configuration for a single share.
type FileData struct {
	Path       string `json:"path"`
	UploadTime int64  `json:"upload_time"`
	Expiration int64  `json:"expiration"`
	Expired    bool   `json:"expired"`
	AllowPost  bool   `json:"allow_post"`
	NoZip      bool   `json:"no_zip,omitempty"`
	Office     string `json:"office,omitempty"`
	Password   string `json:"password"`
}

// IsExpired reports whether a share has expired manually or by its expiration timestamp.
func IsExpired(fd FileData) bool {
	return fd.Expired || (fd.Expiration != 0 && fd.Expiration < time.Now().Unix())
}

// Config is the top-level application configuration as stored in data.json.
type Config struct {
	Port                   int                 `json:"port"`
	MaxPostSize            int                 `json:"maxPostSize"`
	ChunkInactivityTimeout int                 `json:"chunkInactivityTimeout"`
	AdminUsername          string              `json:"admin_username"`
	AdminPassword          string              `json:"admin_password"`
	OfficeURL              string              `json:"office_url,omitempty"`
	OfficeSecret           string              `json:"office_secret,omitempty"`
	Files                  map[string]FileData `json:"files"`
}

// ApplyDefaults fills zero-value fields that would break the server if left unset.
func (c *Config) ApplyDefaults() {
	if c.Files == nil {
		c.Files = make(map[string]FileData)
	}
	def := reflect.ValueOf(defaults)
	target := reflect.ValueOf(c).Elem()
	for i := range target.NumField() {
		if f := target.Field(i); f.IsZero() {
			f.Set(def.Field(i))
		}
	}
}

// Clone returns a copy that shares nothing mutable with c.
func (c *Config) Clone() *Config {
	cp := *c
	cp.Files = maps.Clone(c.Files)
	return &cp
}
