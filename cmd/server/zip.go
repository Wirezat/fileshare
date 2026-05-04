package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	GoLog "github.com/Wirezat/GoLog"
)

const (
	//
	smallFileThreshold = 1 << 20   // Files <= smallFileThreshold (1 MB) are compressed in RAM; larger files stream directly to the ZIP writer.
	maxBufferedBytes   = 256 << 20 // 256 MB Max RAM held by in-flight compressed buffers.
	// Write buffer for the HTTP response.
	httpWriteBufSize = 1 << 20 // 1 MB
	compressLevel    = flate.DefaultCompression
)

// flatePool recycles flate.Writers to reduce GC pressure on small files.
var flatePool = sync.Pool{
	New: func() any {
		w, _ := flate.NewWriter(io.Discard, compressLevel)
		return w
	},
}

type fileJob struct {
	absPath string
	relPath string
	info    os.FileInfo
}

type fileResult struct {
	job        fileJob
	compressed []byte // deflate payload; nil if Store or large file
	rawBytes   []byte // raw data; nil if Deflate or large file
	crc        uint32
	rawSize    uint64
	method     uint16
}

// compressSmall reads a small file into RAM, compresses it, computes CRC32,
// and picks the smaller representation (Store vs. Deflate).
func compressSmall(job fileJob) (*fileResult, error) {
	raw, err := os.ReadFile(job.absPath)
	if err != nil {
		return nil, err
	}
	crc := crc32.ChecksumIEEE(raw)

	var buf bytes.Buffer
	buf.Grow(len(raw))

	fw := flatePool.Get().(*flate.Writer)
	fw.Reset(&buf)
	_, werr := fw.Write(raw)
	cerr := fw.Close()
	flatePool.Put(fw)

	if werr != nil {
		return nil, werr
	}
	if cerr != nil {
		return nil, cerr
	}

	if buf.Len() >= len(raw) {
		// Compressed is larger — use Store.
		return &fileResult{
			job: job, rawBytes: raw,
			crc: crc, rawSize: uint64(len(raw)), method: zip.Store,
		}, nil
	}
	return &fileResult{
		job: job, compressed: buf.Bytes(),
		crc: crc, rawSize: uint64(len(raw)), method: zip.Deflate,
	}, nil
}

// zipAndServe streams a ZIP archive of dirPath directly to the HTTP client.
//
// Small files (≤ smallFileThreshold) are compressed in parallel by a worker pool
// and written via CreateRaw — no double-compression. Large files are streamed
// straight from disk by the ZIP writer. Backpressure keeps RAM usage bounded.
func zipAndServe(w http.ResponseWriter, dirPath string) {
	folderName := filepath.Base(filepath.Clean(dirPath))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s.zip"`, folderName))

	numWorkers := runtime.NumCPU()
	if numWorkers < 1 {
		numWorkers = 1
	}

	jobs := make(chan fileJob, numWorkers*8)
	results := make(chan *fileResult, numWorkers*8)

	// Tracks bytes compressed in RAM but not yet consumed by the ZIP writer.
	var bufferedBytes atomic.Int64

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if job.info.Size() > smallFileThreshold {
					// Large file: send a marker and let the ZIP writer stream it.
					results <- &fileResult{job: job}
					continue
				}

				r, err := compressSmall(job)
				if err != nil {
					GoLog.Errorf("compress %s: %v", job.relPath, err)
					continue
				}

				// Backpressure: block until the ZIP writer drains enough buffered data.
				var payloadSize int64
				if r.method == zip.Deflate {
					payloadSize = int64(len(r.compressed))
				} else {
					payloadSize = int64(len(r.rawBytes))
				}
				for bufferedBytes.Load() > maxBufferedBytes {
					runtime.Gosched()
				}
				bufferedBytes.Add(payloadSize)
				results <- r
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				GoLog.Warnf("walk %s: %v", path, err)
				return nil
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dirPath, path)
			if err != nil {
				GoLog.Warnf("rel path %s: %v", path, err)
				return nil
			}
			jobs <- fileJob{absPath: path, relPath: rel, info: info}
			return nil
		})
		close(jobs)
	}()

	// bufio.Writer batches small writes to reduce syscall overhead.
	bw := bufio.NewWriterSize(w, httpWriteBufSize)
	zw := zip.NewWriter(bw)

	for r := range results {
		hdr, err := zip.FileInfoHeader(r.job.info)
		if err != nil {
			GoLog.Errorf("file info header %s: %v", r.job.relPath, err)
			continue
		}
		hdr.Name = r.job.relPath

		isLargeFile := r.compressed == nil && r.rawBytes == nil

		if isLargeFile {
			// Stream large file directly from disk — no RAM spike.
			hdr.Method = zip.Deflate
			fw, err := zw.CreateHeader(hdr)
			if err != nil {
				GoLog.Errorf("create header %s: %v", r.job.relPath, err)
				continue
			}
			f, err := os.Open(r.job.absPath)
			if err != nil {
				GoLog.Errorf("open %s: %v", r.job.absPath, err)
				continue
			}
			if _, err := io.Copy(fw, f); err != nil {
				GoLog.Errorf("stream %s: %v", r.job.relPath, err)
			}
			f.Close()
			_ = bw.Flush() // flush after each large file so the client sees progress
			continue
		}

		// Write pre-compressed data via CreateRaw to avoid double-compression.
		// CRC and sizes were already computed by compressSmall.
		hdr.Method = r.method
		hdr.CRC32 = r.crc
		hdr.UncompressedSize64 = r.rawSize
		hdr.Flags &^= 0x8 // clear data-descriptor flag; sizes are in the local header

		var payload []byte
		if r.method == zip.Deflate {
			hdr.CompressedSize64 = uint64(len(r.compressed))
			payload = r.compressed
		} else {
			hdr.CompressedSize64 = r.rawSize
			payload = r.rawBytes
		}

		fw, err := zw.CreateRaw(hdr)
		if err != nil {
			GoLog.Errorf("create raw %s: %v", r.job.relPath, err)
			bufferedBytes.Add(-int64(len(payload)))
			continue
		}
		if _, err := fw.Write(payload); err != nil {
			GoLog.Errorf("write raw %s: %v", r.job.relPath, err)
		}
		bufferedBytes.Add(-int64(len(payload)))
	}

	if err := zw.Close(); err != nil {
		GoLog.Errorf("zip close: %v", err)
	}
	_ = bw.Flush()

	GoLog.Infof("zip served: %s (%d workers)", folderName, numWorkers)
}
