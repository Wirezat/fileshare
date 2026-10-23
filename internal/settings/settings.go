package settings

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

const defaultPort = 27182

// Settings holds operating settings read from FS_* environment variables.
// Port 0 means not set; LogDir "" means the GoLog default next to the binary.
type Settings struct {
	Port    int
	Bind    string
	DataDir string
	LogDir  string
}

// Load reads settings through getenv, which maps a variable name to its value ("" when unset).
// It returns an error naming the variable when a value is invalid.
func Load(getenv func(string) string) (Settings, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Settings{}, err
	}
	s := Settings{Bind: "0.0.0.0", DataDir: cwd}

	if v := getenv("FS_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return Settings{}, fmt.Errorf("FS_PORT=%q: want a port number 1-65535", v)
		}
		s.Port = p
	}
	if v := getenv("FS_BIND"); v != "" {
		if net.ParseIP(v) == nil {
			return Settings{}, fmt.Errorf("FS_BIND=%q: want an IP address", v)
		}
		s.Bind = v
	}
	if v := getenv("FS_DATA_DIR"); v != "" {
		if s.DataDir, err = filepath.Abs(v); err != nil {
			return Settings{}, fmt.Errorf("FS_DATA_DIR=%q: %w", v, err)
		}
	}
	if v := getenv("FS_LOG_DIR"); v != "" {
		if s.LogDir, err = filepath.Abs(v); err != nil {
			return Settings{}, fmt.Errorf("FS_LOG_DIR=%q: %w", v, err)
		}
	}
	return s, nil
}

// ResolvePort returns the port to listen on: Port when set, else fromConfig when non-zero, else 27182.
func (s Settings) ResolvePort(fromConfig int) int {
	switch {
	case s.Port != 0:
		return s.Port
	case fromConfig != 0:
		return fromConfig
	default:
		return defaultPort
	}
}
