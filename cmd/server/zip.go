package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	GoLog "github.com/Wirezat/GoLog"
)

type fileJob struct {
	path    string
	relPath string
}

type compressedResult struct {
	relPath string
	data    []byte
	method  uint16
}

// compressFile reads and compresses a single file into a buffer.
func compressFile(job fileJob) (*compressedResult, error) {
	data, err := os.ReadFile(job.path)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, err
	}
	w.Close()

	// Use Store if compression doesn't help
	if buf.Len() >= len(data) {
		return &compressedResult{relPath: job.relPath, data: data, method: zip.Store}, nil
	}
	return &compressedResult{relPath: job.relPath, data: buf.Bytes(), method: zip.Deflate}, nil
}

// zipAndServe streams a ZIP archive of dirPath to the client.
// Files are compressed in parallel; results are written sequentially.
func zipAndServe(w http.ResponseWriter, dirPath string) {
	folderName := filepath.Base(filepath.Clean(dirPath))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", folderName))

	numWorkers := runtime.NumCPU() - 1
	if numWorkers < 1 {
		numWorkers = 1
	}

	jobs := make(chan fileJob, numWorkers*2)
	results := make(chan *compressedResult, numWorkers*2)

	// Workers: parallel compress into buffers
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				info, err := os.Stat(job.path)
				if err != nil || info.IsDir() {
					continue
				}
				result, err := compressFile(job)
				if err != nil {
					GoLog.Errorf("failed to compress %s: %v", job.relPath, err)
					continue
				}
				results <- result
			}
		}()
	}

	// Close results once all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Walk in background
	go func() {
		err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				GoLog.Warnf("walk error at %s: %v", path, err)
				return nil
			}
			if path == dirPath {
				return nil
			}
			relPath, err := filepath.Rel(dirPath, path)
			if err != nil {
				GoLog.Warnf("failed to get relative path for %s: %v", path, err)
				return nil
			}
			jobs <- fileJob{path: path, relPath: relPath}
			return nil
		})
		if err != nil {
			GoLog.Errorf("walk failed for %s: %v", dirPath, err)
		}
		close(jobs)
	}()

	// Sequential ZIP writer consuming compressed results
	pr, pw := io.Pipe()
	go func() {
		zw := zip.NewWriter(pw)
		for result := range results {
			hdr := &zip.FileHeader{
				Name:   result.relPath,
				Method: result.method,
			}
			fw, err := zw.CreateHeader(hdr)
			if err != nil {
				GoLog.Errorf("failed to create zip entry for %s: %v", result.relPath, err)
				continue
			}
			if _, err := fw.Write(result.data); err != nil {
				GoLog.Errorf("failed to write zip entry for %s: %v", result.relPath, err)
			}
		}
		zw.Close()
		pw.Close()
	}()

	if _, err := io.Copy(w, pr); err != nil {
		GoLog.Errorf("failed to stream zip to client: %v", err)
	}

	GoLog.Infof("zip served: %s (%d workers)", folderName, numWorkers)
}
