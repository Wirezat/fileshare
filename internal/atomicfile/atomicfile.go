// Package atomicfile replaces files so that readers see either the old or the new content.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Replace writes the content of src to a hidden temporary file next to the file at path and renames it over that
// file, keeping its mode and writing through any symlink. limit caps the size in bytes; a larger src leaves the file
// untouched and gives an error.
func Replace(path string, src io.Reader, limit int64) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("save target is a directory")
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".fileshare-*.tmp")
	if err != nil {
		return err
	}
	n, err := io.Copy(tmp, io.LimitReader(src, limit+1))
	if err == nil && n > limit {
		err = fmt.Errorf("content exceeds %d bytes", limit)
	}
	if err == nil {
		err = tmp.Chmod(info.Mode().Perm())
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
