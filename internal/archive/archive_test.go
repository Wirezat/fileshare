package archive

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func unzip(t *testing.T, data []byte) (map[string]string, []*zip.File) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = string(b)
	}
	return out, zr.File
}

func write(t *testing.T, dir string, roots []string) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, dir, roots, func(name string) bool { return name == "skip.me" }); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, _ := unzip(t, buf.Bytes())
	return got
}

func TestWriteIncludesHiddenAndLeavesOutSkipped(t *testing.T) {
	dir := tree(t, map[string]string{
		"a.txt": "a", "sub/b.txt": "b",
		".secret": "s", "sub/.hidden": "h", ".git/HEAD": "ref",
		"skip.me": "x", "sub/skip.me": "y",
	})
	want := map[string]string{"a.txt": "a", "sub/b.txt": "b", ".secret": "s", "sub/.hidden": "h", ".git/HEAD": "ref"}
	if got := write(t, dir, nil); !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWriteOnlyRoots(t *testing.T) {
	dir := tree(t, map[string]string{"a.txt": "a", "sub/b.txt": "b", "c.txt": "c"})
	want := map[string]string{"a.txt": "a", "sub/b.txt": "b"}
	if got := write(t, dir, []string{"a.txt", "sub"}); !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWriteLargeFileRoundTrips(t *testing.T) {
	big := strings.Repeat("0123456789", 200_000)
	dir := tree(t, map[string]string{"big.txt": big, "small.txt": "s"})
	got := write(t, dir, nil)
	if got["big.txt"] != big || got["small.txt"] != "s" {
		t.Errorf("content mismatch: big %d bytes, small %q", len(got["big.txt"]), got["small.txt"])
	}
}

func TestWriteStoresIncompressibleSmallFile(t *testing.T) {
	noise := make([]byte, 4096)
	rand.Read(noise)
	dir := tree(t, map[string]string{"noise.bin": string(noise)})
	var buf bytes.Buffer
	if err := Write(&buf, dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, files := unzip(t, buf.Bytes())
	if got["noise.bin"] != string(noise) {
		t.Error("content mismatch")
	}
	if files[0].Method != zip.Store {
		t.Errorf("method = %d, want Store", files[0].Method)
	}
}

func TestWriteEmptyDirectory(t *testing.T) {
	if got := write(t, t.TempDir(), nil); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }

func TestWriteReportsWriterFailureWithoutHanging(t *testing.T) {
	files := map[string]string{"big.txt": strings.Repeat("x", 3<<20)}
	for i := range 50 {
		files[filepath.Join("many", strings.Repeat("f", i+1)+".txt")] = "data"
	}
	dir := tree(t, files)
	done := make(chan error, 1)
	go func() { done <- Write(failingWriter{}, dir, nil, nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Write returned nil on a failing writer")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Write hung on a failing writer")
	}
}

func TestByteBudgetBlocksUntilRelease(t *testing.T) {
	b := newByteBudget(10)
	b.acquire(8)
	b.acquire(8)

	got := make(chan struct{})
	go func() {
		b.acquire(1)
		close(got)
	}()

	select {
	case <-got:
		t.Fatal("acquire went through while the budget was exceeded")
	case <-time.After(50 * time.Millisecond):
	}

	b.release(3)
	select {
	case <-got:
		t.Fatal("acquire went through while the budget was still exceeded")
	case <-time.After(50 * time.Millisecond):
	}

	b.release(13)
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("acquire still blocked after release")
	}
}

func TestWriteLeavesOutWhatItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	dir := tree(t, map[string]string{"ok.txt": "ok", "big.bin": strings.Repeat("x", smallFileThreshold+1)})
	if err := os.Chmod(filepath.Join(dir, "big.bin"), 0); err != nil {
		t.Fatal(err)
	}
	if got := write(t, dir, nil); len(got) != 1 || got["ok.txt"] != "ok" {
		t.Errorf("entries = %v, want only ok.txt", len(got))
	}
}
