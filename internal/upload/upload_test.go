package upload

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const id = "upload-0001"

func newStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	s := New(filepath.Join(t.TempDir(), "chunks"), func() time.Duration { return time.Hour })
	return s, t.TempDir()
}

func send(t *testing.T, s *Storage, index int, data string) bool {
	t.Helper()
	done, err := s.Receive(id, index, strings.NewReader(data))
	if err != nil {
		t.Fatalf("Receive(%d): %v", index, err)
	}
	return done
}

func initSession(t *testing.T, s *Storage, filename string, total int, dest string) []int {
	t.Helper()
	missing, err := s.Init(id, filename, total, dest)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return missing
}

func TestInitReportsAllChunksMissing(t *testing.T) {
	s, dest := newStorage(t)
	if missing := initSession(t, s, "a.txt", 3, dest); !slices.Equal(missing, []int{0, 1, 2}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestReceiveAssemblesOutOfOrderChunks(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 3, dest)
	if send(t, s, 2, "ccc") || send(t, s, 0, "aaa") {
		t.Fatal("done before the last chunk")
	}
	if !send(t, s, 1, "bbb") {
		t.Fatal("not done after the last chunk")
	}
	got, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(got) != "aaabbbccc" {
		t.Errorf("file = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(s.tempBase, id)); !os.IsNotExist(err) {
		t.Error("session directory left behind")
	}
}

func TestInitResumesFromDiskAfterRestart(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 3, dest)
	send(t, s, 0, "aaa")
	restarted := New(s.tempBase, s.timeout)
	if missing := initSession(t, restarted, "a.txt", 3, dest); !slices.Equal(missing, []int{1, 2}) {
		t.Errorf("missing after restart = %v, want [1 2]", missing)
	}
}

func TestDuplicateChunkIsIgnored(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 2, dest)
	send(t, s, 0, "aa")
	if send(t, s, 0, "XX") {
		t.Fatal("duplicate chunk completed the upload")
	}
	send(t, s, 1, "bb")
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "aabb" {
		t.Errorf("file = %q", got)
	}
}

func TestRejectsBadIDsAndIndexes(t *testing.T) {
	s, dest := newStorage(t)
	for _, bad := range []string{"", "short", "../../../etc", "has/slash-0001", strings.Repeat("a", 129)} {
		if _, err := s.Init(bad, "a.txt", 1, dest); !errors.Is(err, ErrBadID) {
			t.Errorf("Init(%q) err = %v, want ErrBadID", bad, err)
		}
	}
	initSession(t, s, "a.txt", 2, dest)
	for _, i := range []int{-1, 2} {
		if _, err := s.Receive(id, i, strings.NewReader("x")); err == nil {
			t.Errorf("Receive index %d accepted", i)
		}
	}
	if _, err := s.Receive("unknown-0001", 0, strings.NewReader("x")); err == nil {
		t.Error("Receive for unknown session accepted")
	}
}

func TestFilenameCannotLeaveDestination(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "../../evil.txt", 1, dest)
	send(t, s, 0, "x")
	if _, err := os.Stat(filepath.Join(dest, "evil.txt")); err != nil {
		t.Errorf("file not written inside destination: %v", err)
	}
}

func TestExistingFileIsNotOverwritten(t *testing.T) {
	s, dest := newStorage(t)
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	initSession(t, s, "a.txt", 1, dest)
	send(t, s, 0, "new")
	if old, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(old) != "old" {
		t.Errorf("existing file overwritten: %q", old)
	}
	if matches, _ := filepath.Glob(filepath.Join(dest, "a_*.txt")); len(matches) != 1 {
		t.Errorf("suffixed copies = %v, want one", matches)
	}
}

func TestSweepRemovesOnlyIdleSessions(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 2, dest)
	sessionPath := filepath.Join(s.tempBase, id)
	s.sweep(time.Now())
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatal("fresh session swept")
	}
	s.sweep(time.Now().Add(2 * time.Hour))
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("idle session kept")
	}
	if _, err := s.Receive(id, 0, strings.NewReader("x")); err == nil {
		t.Error("swept session still accepts chunks")
	}
}

func TestSweepRemovesDirectoriesWithoutMeta(t *testing.T) {
	s, _ := newStorage(t)
	stray := filepath.Join(s.tempBase, "stray-0001")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}
	s.Sweep()
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Error("directory without meta.json kept")
	}
}

func TestInitSweepsIdleSessionsOncePerInterval(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "chunks"), func() time.Duration { return 0 })
	dest := t.TempDir()
	idle := filepath.Join(s.tempBase, id)
	initSession(t, s, "a.txt", 2, dest)
	if _, err := s.Init("other-0001", "b.txt", 1, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(idle); err != nil {
		t.Fatal("swept again before the interval passed")
	}
	s.swept = time.Now().Add(-sweepInterval - time.Second)
	if _, err := s.Init("other-0001", "b.txt", 1, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Error("idle session survived the sweep in Init")
	}
}
