package access

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var faccessat2 = unix.Faccessat2

func check(path string, mode uint32) bool {
	return faccessat2(unix.AT_FDCWD, path, mode, unix.AT_EACCESS) == nil
}

// Readable reports whether the process may read path (directories also need search).
func Readable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		return check(path, unix.R_OK|unix.X_OK)
	}
	return check(path, unix.R_OK)
}

func Writable(dir string) bool {
	return check(dir, unix.W_OK|unix.X_OK)
}

func UploadDir(path string) string {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

// SelfTest returns the faccessat2 error for dir, nil when the syscall works.
func SelfTest(dir string) error {
	return faccessat2(unix.AT_FDCWD, dir, unix.F_OK, unix.AT_EACCESS)
}
