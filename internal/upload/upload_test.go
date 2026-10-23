package upload

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const id = "upload-0001"

var testDest = map[*Storage]string{}

func TestMain(m *testing.M) {
	minChunkSize = 1
	os.Exit(m.Run())
}

func newStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	s := New(filepath.Join(t.TempDir(), "chunks"), func() time.Duration { return time.Hour })
	dest := t.TempDir()
	testDest[s] = dest
	return s, dest
}

func sessionDir(t *testing.T, s *Storage) string {
	t.Helper()
	dirs, _ := filepath.Glob(filepath.Join(s.tempBase, "*"))
	if len(dirs) != 1 {
		t.Fatalf("session dirs = %v, want one", dirs)
	}
	return dirs[0]
}

func send(t *testing.T, s *Storage, index int, data string) bool {
	t.Helper()
	done, err := s.Receive(id, testDest[s], index, strings.NewReader(data))
	if err != nil {
		t.Fatalf("Receive(%d): %v", index, err)
	}
	return done
}

func initSession(t *testing.T, s *Storage, filename string, size, chunkSize int64, dest string) []int {
	t.Helper()
	missing, err := s.Init(id, filename, size, chunkSize, dest)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return missing
}

func hiddenFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestInitReportsAllChunksMissing(t *testing.T) {
	s, dest := newStorage(t)
	if missing := initSession(t, s, "a.txt", 9, 3, dest); !slices.Equal(missing, []int{0, 1, 2}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestReceiveWritesOutOfOrderChunksInPlace(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 9, 3, dest)
	if send(t, s, 2, "ccc") || send(t, s, 0, "aaa") {
		t.Fatal("done before the last chunk")
	}
	if entries, _ := os.ReadDir(sessionDir(t, s)); len(entries) != 2 {
		t.Errorf("session directory holds %v, want meta.json and the received log only", entries)
	}
	if !send(t, s, 1, "bbb") {
		t.Fatal("not done after the last chunk")
	}
	got, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(got) != "aaabbbccc" {
		t.Errorf("file = %q, %v", got, err)
	}
	if dirs, _ := filepath.Glob(filepath.Join(s.tempBase, "*")); len(dirs) != 0 {
		t.Errorf("session directories left behind: %v", dirs)
	}
	if h := hiddenFiles(t, dest); len(h) != 0 {
		t.Errorf("partial files left behind: %v", h)
	}
}

func TestLastChunkMayBeShorter(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 7, 3, dest)
	send(t, s, 0, "aaa")
	send(t, s, 2, "c")
	if !send(t, s, 1, "bbb") {
		t.Fatal("not done")
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "aaabbbc" {
		t.Errorf("file = %q", got)
	}
}

func TestEmptyFileNeedsOneEmptyChunk(t *testing.T) {
	s, dest := newStorage(t)
	if missing := initSession(t, s, "empty.txt", 0, 3, dest); !slices.Equal(missing, []int{0}) {
		t.Fatalf("missing = %v, want [0]", missing)
	}
	if !send(t, s, 0, "") {
		t.Fatal("not done")
	}
	if fi, err := os.Stat(filepath.Join(dest, "empty.txt")); err != nil || fi.Size() != 0 {
		t.Errorf("empty file: %v, %v", fi, err)
	}
}

func TestChunkOfWrongLengthIsRefused(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 6, 3, dest)
	for _, data := range []string{"aa", "aaaa"} {
		if _, err := s.Receive(id, dest, 0, strings.NewReader(data)); !errors.Is(err, ErrChunkLength) {
			t.Errorf("chunk %q: err = %v, want ErrChunkLength", data, err)
		}
	}
	if missing := initSession(t, s, "a.txt", 6, 3, dest); !slices.Equal(missing, []int{0, 1}) {
		t.Errorf("missing after refused chunks = %v, want [0 1]", missing)
	}
	send(t, s, 0, "aaa")
	send(t, s, 1, "bbb")
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "aaabbb" {
		t.Errorf("file = %q", got)
	}
}

func TestInitResumesFromDiskAfterRestart(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 9, 3, dest)
	send(t, s, 0, "aaa")
	restarted := New(s.tempBase, s.timeout)
	testDest[restarted] = dest
	if missing := initSession(t, restarted, "a.txt", 9, 3, dest); !slices.Equal(missing, []int{1, 2}) {
		t.Errorf("missing after restart = %v, want [1 2]", missing)
	}
	if _, err := restarted.Receive(id, dest, 1, strings.NewReader("bbb")); err != nil {
		t.Fatal(err)
	}
	if done, err := restarted.Receive(id, dest, 2, strings.NewReader("ccc")); err != nil || !done {
		t.Fatalf("Receive = %v, %v", done, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "aaabbbccc" {
		t.Errorf("file = %q", got)
	}
}

func TestDuplicateChunkIsIgnored(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 4, 2, dest)
	send(t, s, 0, "aa")
	if send(t, s, 0, "XX") {
		t.Fatal("duplicate chunk completed the upload")
	}
	send(t, s, 1, "bb")
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "aabb" {
		t.Errorf("file = %q", got)
	}
}

func TestRejectsBadIDsSizesAndIndexes(t *testing.T) {
	s, dest := newStorage(t)
	for _, bad := range []string{"", "short", "../../../etc", "has/slash-0001", strings.Repeat("a", 129)} {
		if _, err := s.Init(bad, "a.txt", 1, 1, dest); !errors.Is(err, ErrBadID) {
			t.Errorf("Init(%q) err = %v, want ErrBadID", bad, err)
		}
	}
	for _, c := range []struct{ size, chunk int64 }{
		{-1, 3}, {3, 0}, {3, MaxChunkSize + 1}, {int64(MaxTotalChunks) + 1, 1},
	} {
		if _, err := s.Init(id, "a.txt", c.size, c.chunk, dest); !errors.Is(err, ErrBadSize) {
			t.Errorf("Init(size %d, chunk %d) err = %v, want ErrBadSize", c.size, c.chunk, err)
		}
	}
	initSession(t, s, "a.txt", 2, 1, dest)
	for _, i := range []int{-1, 2} {
		if _, err := s.Receive(id, dest, i, strings.NewReader("x")); err == nil {
			t.Errorf("Receive index %d accepted", i)
		}
	}
	if _, err := s.Receive("unknown-0001", dest, 0, strings.NewReader("x")); !errors.Is(err, ErrUnknownSession) {
		t.Errorf("Receive for unknown session err = %v, want ErrUnknownSession", err)
	}
}

func TestFilenameCannotLeaveDestination(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "../../evil.txt", 1, 1, dest)
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
	initSession(t, s, "a.txt", 3, 3, dest)
	send(t, s, 0, "new")
	if old, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(old) != "old" {
		t.Errorf("existing file overwritten: %q", old)
	}
	if matches, _ := filepath.Glob(filepath.Join(dest, "a_*.txt")); len(matches) != 1 {
		t.Errorf("suffixed copies = %v, want one", matches)
	}
}

func TestTwoUploadsOfTheSameNameBothSurvive(t *testing.T) {
	s, dest := newStorage(t)
	for _, uid := range []string{"first-0001", "second-0001"} {
		if _, err := s.Init(uid, "a.txt", 2, 2, dest); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, c := range []struct{ uid, data string }{{"first-0001", "11"}, {"second-0001", "22"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Receive(c.uid, dest, 0, strings.NewReader(c.data)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(dest, "a*.txt"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		got[string(data)] = true
	}
	if len(files) != 2 || !got["11"] || !got["22"] {
		t.Errorf("files %v with contents %v, want both uploads kept", files, got)
	}
}

func TestPartialFileIsHiddenAndNamedAtRandom(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 4, 2, dest)
	send(t, s, 0, "aa")
	h := hiddenFiles(t, dest)
	if len(h) != 1 || strings.Contains(h[0], id) || strings.Contains(h[0], "a.txt") {
		t.Fatalf("partial files = %v, want one hidden file named without upload id or filename", h)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); !os.IsNotExist(err) {
		t.Error("final name exists before the upload is complete")
	}
}

func TestConcurrentChunksAreAllWritten(t *testing.T) {
	s, dest := newStorage(t)
	const n = 64
	initSession(t, s, "a.txt", 2*n, 2, dest)
	var wg sync.WaitGroup
	var doneCount sync.Map
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done, err := s.Receive(id, dest, i, strings.NewReader(fmt.Sprintf("%02d", i)))
			if err != nil {
				t.Error(err)
			}
			if done {
				doneCount.Store(i, true)
			}
		}()
	}
	wg.Wait()
	finished := 0
	doneCount.Range(func(_, _ any) bool { finished++; return true })
	if finished != 1 {
		t.Errorf("%d chunks reported completion, want exactly one", finished)
	}
	var want strings.Builder
	for i := range n {
		fmt.Fprintf(&want, "%02d", i)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != want.String() {
		t.Errorf("file = %q", got)
	}
}

func TestLegacySessionIsStartedAfresh(t *testing.T) {
	s, dest := newStorage(t)
	dir := filepath.Join(s.tempBase, s.key(id, dest))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dest, ".fileshare-upload-old.part"), make([]byte, 4), 0o644)
	legacy, _ := json.Marshal(map[string]any{"uploadId": id, "filename": "a.txt", "destDir": dest, "size": 4, "chunkSize": 2,
		"part": filepath.Join(dest, ".fileshare-upload-old.part"), "received": []int{0}, "lastActivity": time.Now()})
	os.WriteFile(filepath.Join(dir, "meta.json"), legacy, 0o600)
	os.WriteFile(filepath.Join(dir, "00000"), []byte("old"), 0o600)

	if missing := initSession(t, s, "a.txt", 4, 2, dest); !slices.Equal(missing, []int{0, 1}) {
		t.Errorf("missing = %v, want a fresh session [0 1]", missing)
	}
	if _, err := os.Stat(filepath.Join(dir, "00000")); !os.IsNotExist(err) {
		t.Error("legacy chunk file kept")
	}
}

func TestSweepRemovesOnlyIdleSessions(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 2, 1, dest)
	sessionPath := sessionDir(t, s)
	s.sweep(time.Now())
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatal("fresh session swept")
	}
	if len(hiddenFiles(t, dest)) != 1 {
		t.Fatal("partial file missing while the session is fresh")
	}
	s.sweep(time.Now().Add(2 * time.Hour))
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("idle session kept")
	}
	if h := hiddenFiles(t, dest); len(h) != 0 {
		t.Errorf("partial file of the idle session kept: %v", h)
	}
	if _, err := s.Receive(id, dest, 0, strings.NewReader("x")); err == nil {
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
	testDest[s] = dest
	initSession(t, s, "a.txt", 2, 1, dest)
	idle := filepath.Join(s.tempBase, s.key(id, dest))
	if _, err := s.Init("other-0001", "b.txt", 1, 1, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(idle); err != nil {
		t.Fatal("swept again before the interval passed")
	}
	s.swept = time.Now().Add(-sweepInterval - time.Second)
	if _, err := s.Init("other-0001", "b.txt", 1, 1, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Error("idle session survived the sweep in Init")
	}
}

func TestSweepNeverRemovesASessionInitIsCreating(t *testing.T) {
	s, dest := newStorage(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				s.sweep(time.Now())
			}
		}
	}()
	ids := make([]string, 300)
	for i := range ids {
		ids[i] = fmt.Sprintf("race-%05d", i)
		if _, err := s.Init(ids[i], "a.txt", 1, 1, dest); err != nil {
			t.Fatalf("Init: %v", err)
		}
	}
	close(stop)
	<-done
	for _, uid := range ids {
		if _, err := os.Stat(filepath.Join(s.tempBase, s.key(uid, dest), "meta.json")); err != nil {
			t.Fatalf("session %s registered but its directory was swept", uid)
		}
	}
}

type failAfter struct {
	data string
	err  error
}

func (f *failAfter) Read(p []byte) (int, error) {
	if f.data == "" {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func TestReadErrorAfterTheChunkIsNotIgnored(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 3, 3, dest)
	boom := errors.New("body too large")
	if _, err := s.Receive(id, dest, 0, &failAfter{data: "aaa", err: boom}); !errors.Is(err, boom) {
		t.Errorf("Receive = %v, want the reader's error", err)
	}
	if missing := initSession(t, s, "a.txt", 3, 3, dest); !slices.Equal(missing, []int{0}) {
		t.Errorf("missing = %v, want the chunk still missing", missing)
	}
}

func TestSameFileInTwoDestinationsIsKeptApart(t *testing.T) {
	s, destA := newStorage(t)
	destB := t.TempDir()
	if _, err := s.Init(id, "f.txt", 4, 2, destA); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Receive(id, destA, 0, strings.NewReader("AA")); err != nil {
		t.Fatal(err)
	}
	if missing, err := s.Init(id, "f.txt", 4, 2, destB); err != nil || !slices.Equal(missing, []int{0, 1}) {
		t.Fatalf("Init in B = %v, %v; want a separate session", missing, err)
	}
	if done, err := s.Receive(id, destA, 1, strings.NewReader("aa")); err != nil || !done {
		t.Fatalf("A's last chunk = %v, %v", done, err)
	}
	if got, _ := os.ReadFile(filepath.Join(destA, "f.txt")); string(got) != "AAaa" {
		t.Errorf("A's file = %q", got)
	}
	if _, err := os.Stat(filepath.Join(destB, "f.txt")); !os.IsNotExist(err) {
		t.Error("A's upload landed in B")
	}
}

func TestChunkForAnotherDestinationIsUnknown(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 2, 1, dest)
	if _, err := s.Receive(id, t.TempDir(), 0, strings.NewReader("x")); !errors.Is(err, ErrUnknownSession) {
		t.Errorf("chunk sent to another destination: err = %v, want ErrUnknownSession", err)
	}
}

func TestTheLastChunkSentTwiceAtOnceSucceedsBothTimes(t *testing.T) {
	for range 20 {
		s, dest := newStorage(t)
		initSession(t, s, "a.txt", 4, 2, dest)
		send(t, s, 0, "aa")
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.Receive(id, dest, 1, strings.NewReader("bb")); err != nil && !errors.Is(err, ErrUnknownSession) {
					t.Errorf("duplicate last chunk: %v", err)
				}
			}()
		}
		wg.Wait()
		if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "aabb" {
			t.Fatalf("file = %q", got)
		}
	}
}

func TestTinyChunksAreRefusedUnlessTheFileFitsInOne(t *testing.T) {
	prev := minChunkSize
	minChunkSize = 64 << 10
	defer func() { minChunkSize = prev }()
	s, dest := newStorage(t)
	if _, err := s.Init(id, "a.txt", 200_000, 1, dest); !errors.Is(err, ErrBadSize) {
		t.Errorf("1-byte chunks for a 200 KB file: err = %v, want ErrBadSize", err)
	}
	if _, err := s.Init("tiny-0001", "t.txt", 3, 5, dest); err != nil {
		t.Errorf("single small chunk refused: %v", err)
	}
}

func TestReceivingAChunkDoesNotRewriteMeta(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 6, 2, dest)
	meta := filepath.Join(sessionDir(t, s), "meta.json")
	before, _ := os.ReadFile(meta)
	send(t, s, 0, "aa")
	send(t, s, 2, "cc")
	if after, _ := os.ReadFile(meta); string(after) != string(before) {
		t.Error("meta.json rewritten for a chunk")
	}
}

func TestInitKeepsAResumedSessionAlive(t *testing.T) {
	s, dest := newStorage(t)
	initSession(t, s, "a.txt", 4, 2, dest)
	send(t, s, 0, "aa")
	time.Sleep(10 * time.Millisecond)
	cut := time.Now()
	initSession(t, s, "a.txt", 4, 2, dest)
	s.sweep(cut.Add(time.Hour - time.Millisecond))
	if missing := initSession(t, s, "a.txt", 4, 2, dest); !slices.Equal(missing, []int{1}) {
		t.Errorf("missing after resume and sweep = %v, want [1]", missing)
	}
}

func TestChunkIsNotWrittenThroughAPartFileSwappedForASymlink(t *testing.T) {
	s, dest := newStorage(t)
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	initSession(t, s, "a.txt", 4, 4, dest)
	parts := hiddenFiles(t, dest)
	if len(parts) != 1 {
		t.Fatalf("part files = %v", parts)
	}
	if err := os.Remove(parts[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, parts[0]); err != nil {
		t.Fatal(err)
	}
	s.Receive(id, dest, 0, strings.NewReader("evil"))
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Errorf("chunk written through the swapped part file: %q", got)
	}
}
