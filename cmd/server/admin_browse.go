package main

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/Wirezat/GoLog"
)

type browseEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

type browseResult struct {
	Path    string        `json:"path"`
	Entries []browseEntry `json:"entries"`
}

// handleAdminBrowse lists the immediate children of an absolute directory,
// folders first. GET /admin/api/browse?path=<dir>
func handleAdminBrowse(w http.ResponseWriter, r *http.Request) {
	if !methodOnly(w, r, http.MethodGet) {
		return
	}
	raw := r.URL.Query().Get("path")
	if !filepath.IsAbs(raw) {
		http.Error(w, "absolute path required", http.StatusBadRequest)
		return
	}
	dir := filepath.Clean(raw)

	info, err := os.Stat(dir)
	if err != nil {
		browseError(w, dir, err)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	dirents, err := os.ReadDir(dir)
	if err != nil {
		browseError(w, dir, err)
		return
	}

	entries := make([]browseEntry, 0, len(dirents))
	for _, d := range dirents {
		isDir := d.IsDir()
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Stat(filepath.Join(dir, d.Name()))
			isDir = err == nil && target.IsDir()
		}
		entries = append(entries, browseEntry{Name: d.Name(), Dir: isDir})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a != b {
			return a < b
		}
		return entries[i].Name < entries[j].Name
	})
	jsonResponse(w, browseResult{Path: dir, Entries: entries})
}

func browseError(w http.ResponseWriter, dir string, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, fs.ErrPermission):
		http.Error(w, "permission denied", http.StatusForbidden)
	case errors.Is(err, syscall.ENOTDIR):
		http.Error(w, "not a directory", http.StatusBadRequest)
	default:
		GoLog.Errorf("browse %s: %v", dir, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
