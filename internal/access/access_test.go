package access

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func skipRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission checks are meaningless as root")
	}
}

func mkDir(t *testing.T, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o755) })
	return p
}

func mkFile(t *testing.T, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadable(t *testing.T) {
	skipRoot(t)
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"file 0644", mkFile(t, 0o644), true},
		{"file 0000", mkFile(t, 0o000), false},
		{"dir 0755", mkDir(t, 0o755), true},
		{"dir 0300", mkDir(t, 0o300), false},
		{"dir 0600", mkDir(t, 0o600), false},
		{"missing", filepath.Join(t.TempDir(), "nope"), false},
	}
	for _, c := range cases {
		if got := Readable(c.path); got != c.want {
			t.Errorf("%s: Readable = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWritable(t *testing.T) {
	skipRoot(t)
	if !Writable(mkDir(t, 0o755)) {
		t.Error("0755 dir should be writable")
	}
	if Writable(mkDir(t, 0o555)) {
		t.Error("0555 dir should not be writable")
	}
}

func TestUploadDir(t *testing.T) {
	d := t.TempDir()
	if got := UploadDir(d); got != d {
		t.Errorf("dir: got %q, want %q", got, d)
	}
	f := filepath.Join(d, "f")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := UploadDir(f); got != d {
		t.Errorf("file: got %q, want %q", got, d)
	}
}

func TestFailClosed(t *testing.T) {
	orig := faccessat2
	t.Cleanup(func() { faccessat2 = orig })
	d := mkDir(t, 0o755)
	for _, errno := range []error{unix.ENOSYS, unix.EPERM} {
		faccessat2 = func(int, string, uint32, int) error { return errno }
		if Readable(d) {
			t.Errorf("%v: Readable should be false", errno)
		}
		if Writable(d) {
			t.Errorf("%v: Writable should be false", errno)
		}
		if SelfTest(d) == nil {
			t.Errorf("%v: SelfTest should fail", errno)
		}
	}
}

func TestSelfTestOK(t *testing.T) {
	if err := SelfTest(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
