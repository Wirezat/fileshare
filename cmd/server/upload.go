package main

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveUploadedFile saves a single uploaded file to uploadDir.
// If a file with the same name already exists, a unix timestamp is appended.
func saveUploadedFile(fileHeader *multipart.FileHeader, uploadDir string) error {
	src, err := fileHeader.Open()
	if err != nil {
		return fmt.Errorf("error opening uploaded file: %w", err)
	}
	defer src.Close()

	dstPath := filepath.Join(uploadDir, filepath.Base(fileHeader.Filename))
	if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
		timestamp := time.Now().Unix()
		dstPath = filepath.Join(uploadDir, fmt.Sprintf("%s_%d%s",
			strings.TrimSuffix(filepath.Base(fileHeader.Filename), filepath.Ext(fileHeader.Filename)),
			timestamp,
			filepath.Ext(fileHeader.Filename)))
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("error creating file on server: %w", err)
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		return fmt.Errorf("error saving file: %w", err)
	}
	return nil
}
