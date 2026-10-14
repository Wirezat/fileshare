package archive

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wirezat/GoLog"
)

const (
	smallFileThreshold = 1 << 20
	maxBufferedBytes   = 256 << 20
	writeBufSize       = 1 << 20
	compressLevel      = flate.DefaultCompression
)

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
	compressed []byte
	rawBytes   []byte
	crc        uint32
	rawSize    uint64
	method     uint16
}

// compressSmall reads a small file, deflates it and keeps whichever of raw and deflated is smaller.
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

// Write streams a ZIP of dir to w, skipping hidden entries; non-empty roots
// limit it to those paths relative to dir. It returns the error from finishing the archive.
func Write(w io.Writer, dir string, roots []string) error {
	numWorkers := max(runtime.NumCPU(), 1)

	jobs := make(chan fileJob, numWorkers*8)
	results := make(chan *fileResult, numWorkers*8)

	var bufferedBytes atomic.Int64

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if job.info.Size() > smallFileThreshold {
					results <- &fileResult{job: job}
					continue
				}

				r, err := compressSmall(job)
				if err != nil {
					GoLog.Errorf("compress %s: %v", job.relPath, err)
					continue
				}

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
		walk := func(path string, info os.FileInfo, err error) error {
			if err != nil {
				GoLog.Warnf("walk %s: %v", path, err)
				return nil
			}
			if strings.HasPrefix(info.Name(), ".") && path != dir {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				GoLog.Warnf("rel path %s: %v", path, err)
				return nil
			}
			jobs <- fileJob{absPath: path, relPath: rel, info: info}
			return nil
		}
		if len(roots) == 0 {
			_ = filepath.Walk(dir, walk)
		}
		for _, root := range roots {
			_ = filepath.Walk(filepath.Join(dir, root), walk)
		}
		close(jobs)
	}()

	bw := bufio.NewWriterSize(w, writeBufSize)
	zw := zip.NewWriter(bw)

	for r := range results {
		hdr, err := zip.FileInfoHeader(r.job.info)
		if err != nil {
			GoLog.Errorf("file info header %s: %v", r.job.relPath, err)
			continue
		}
		hdr.Name = r.job.relPath

		if r.compressed == nil && r.rawBytes == nil {
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
			_ = bw.Flush()
			continue
		}

		hdr.Method = r.method
		hdr.CRC32 = r.crc
		hdr.UncompressedSize64 = r.rawSize
		hdr.Flags &^= 0x8

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
		return err
	}
	return bw.Flush()
}
