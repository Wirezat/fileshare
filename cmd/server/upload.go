package main

import (
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

// saveUploadedFile saves an uploaded file to uploadDir.
// Collisions are resolved atomically via O_EXCL + UnixNano retry loop.
// Copy buffers are reused via sync.Pool to reduce GC pressure.
func saveUploadedFile(fh *multipart.FileHeader, uploadDir string) error {
	src, err := fh.Open()
	if err != nil {
		GoLog.Errorf("failed to open upload %q: %v", fh.Filename, err)
		return fmt.Errorf("opening uploaded file: %w", err)
	}
	defer src.Close()

	base := filepath.Base(fh.Filename)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	dst := filepath.Join(uploadDir, base)

	var f *os.File
	for i := 0; ; i++ {
		f, err = os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			GoLog.Errorf("failed to create %q: %v", dst, err)
			return fmt.Errorf("creating destination file: %w", err)
		}
		if i >= maxCollisionRetries {
			GoLog.Errorf("exceeded %d retries for %q", maxCollisionRetries, base)
			return fmt.Errorf("too many name collisions for %q", base)
		}
		dst = filepath.Join(uploadDir, fmt.Sprintf("%s_%d%s", stem, time.Now().UnixNano(), ext))
		GoLog.Debugf("collision #%d → retrying as %q", i+1, filepath.Base(dst))
	}
	defer f.Close()

	buf := bufPool.Get().(*[]byte)
	defer bufPool.Put(buf)

	if _, err = io.CopyBuffer(f, src, *buf); err != nil {
		GoLog.Errorf("failed to write %q: %v", dst, err)
		return fmt.Errorf("writing file: %w", err)
	}

	GoLog.Infof("saved %q → %q", fh.Filename, dst)
	return nil
}
