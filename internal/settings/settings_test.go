package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		env     map[string]string
		want    Settings
		wantErr string
	}{
		{
			name: "defaults",
			env:  map[string]string{},
			want: Settings{Port: 0, Bind: "0.0.0.0", DataDir: cwd, LogDir: ""},
		},
		{
			name: "port",
			env:  map[string]string{"FS_PORT": "8080"},
			want: Settings{Port: 8080, Bind: "0.0.0.0", DataDir: cwd},
		},
		{
			name:    "port not a number",
			env:     map[string]string{"FS_PORT": "abc"},
			wantErr: "FS_PORT",
		},
		{
			name:    "port zero",
			env:     map[string]string{"FS_PORT": "0"},
			wantErr: "FS_PORT",
		},
		{
			name:    "port too large",
			env:     map[string]string{"FS_PORT": "70000"},
			wantErr: "FS_PORT",
		},
		{
			name: "bind loopback",
			env:  map[string]string{"FS_BIND": "127.0.0.1"},
			want: Settings{Port: 0, Bind: "127.0.0.1", DataDir: cwd},
		},
		{
			name:    "bind invalid",
			env:     map[string]string{"FS_BIND": "nope"},
			wantErr: "FS_BIND",
		},
		{
			name: "relative data dir",
			env:  map[string]string{"FS_DATA_DIR": "data"},
			want: Settings{Bind: "0.0.0.0", DataDir: filepath.Join(cwd, "data")},
		},
		{
			name: "absolute log dir",
			env:  map[string]string{"FS_LOG_DIR": "/var/log/fs"},
			want: Settings{Bind: "0.0.0.0", DataDir: cwd, LogDir: "/var/log/fs"},
		},
		{
			name: "relative log dir",
			env:  map[string]string{"FS_LOG_DIR": "logs"},
			want: Settings{Bind: "0.0.0.0", DataDir: cwd, LogDir: filepath.Join(cwd, "logs")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(envMap(tt.env))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want error mentioning %s", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Load() error = %q, want mention of %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolvePort(t *testing.T) {
	tests := []struct {
		name       string
		s          Settings
		fromConfig int
		want       int
	}{
		{"nothing set", Settings{Port: 0}, 0, 27182},
		{"config only", Settings{Port: 0}, 9000, 9000},
		{"env wins over config", Settings{Port: 8080}, 9000, 8080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.ResolvePort(tt.fromConfig); got != tt.want {
				t.Errorf("ResolvePort(%d) = %d, want %d", tt.fromConfig, got, tt.want)
			}
		})
	}
}
