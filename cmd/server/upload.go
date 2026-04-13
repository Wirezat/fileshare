package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
)

const maxCollisionRetries = 10

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// Storage is the interface for saving uploaded files.
// LocalStorage is the only implementation for now, but the interface
// makes the code testable and leaves the door open for future backends.
type Storage interface {
	Save(ctx context.Context, fh *multipart.FileHeader) (string, error)
}

// LocalStorage saves files to a directory on the local filesystem.
type LocalStorage struct {
	Dir string
}

// Save writes an uploaded file to LocalStorage.Dir atomically.
// Returns the final filename (may differ from fh.Filename on collision).
func (s *LocalStorage) Save(_ context.Context, fh *multipart.FileHeader) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("opening upload %q: %w", fh.Filename, err)
	}
	defer src.Close()

	base := filepath.Base(fh.Filename)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	dst := filepath.Join(s.Dir, base)

	var f *os.File
	for i := 0; ; i++ {
		f, err = os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("creating %q: %w", dst, err)
		}
		if i >= maxCollisionRetries {
			return "", fmt.Errorf("too many name collisions for %q", base)
		}
		dst = filepath.Join(s.Dir, fmt.Sprintf("%s_%d%s", stem, time.Now().UnixNano(), ext))
		GoLog.Debugf("collision #%d → retrying as %q", i+1, filepath.Base(dst))
	}
	defer f.Close()

	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	if _, err = io.CopyBuffer(f, src, *buf); err != nil {
		os.Remove(dst) // cleanup partial file
		return "", fmt.Errorf("writing %q: %w", dst, err)
	}

	GoLog.Infof("saved %q → %q", fh.Filename, dst)
	return filepath.Base(dst), nil
}
