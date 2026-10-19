package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
)

type testClock struct{ ns atomic.Int64 }

func (c *testClock) now() time.Time          { return time.Unix(0, c.ns.Load()) }
func (c *testClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

func clockedStore(t testing.TB, raw string) (*FileStore, string, *testClock, *atomic.Int32) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &testClock{}
	c.ns.Store(time.Now().UnixNano())
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var stats atomic.Int32
	s.now = c.now
	s.start = c.now()
	s.stat = func(p string) (os.FileInfo, error) {
		stats.Add(1)
		return os.Stat(p)
	}
	return s, path, c, &stats
}

func handEdit(t testing.TB, path, raw string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func captureLogs(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	lines := &[]string{}
	prev := GoLog.OnRecord
	GoLog.OnRecord = func(r GoLog.Record) {
		mu.Lock()
		*lines = append(*lines, r.Message)
		mu.Unlock()
	}
	t.Cleanup(func() { GoLog.OnRecord = prev })
	return lines
}

func count(lines []string, part string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, part) {
			n++
		}
	}
	return n
}

func TestHandEditShowsOnceTheIntervalHasPassed(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	handEdit(t, path, `{"files":{"hand":{"path":"/srv/hand"}}}`)

	if _, ok := s.Current().Share("hand"); ok {
		t.Error("hand edit visible before the interval passed")
	}
	c.advance(checkInterval)
	if _, ok := s.Current().Share("hand"); !ok {
		t.Error("hand edit not visible after the interval")
	}
}

func TestParallelReadersCheckTheFileOncePerInterval(t *testing.T) {
	s, _, c, stats := clockedStore(t, `{"files":{}}`)
	c.advance(checkInterval)

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { s.Current() })
	}
	wg.Wait()

	if n := stats.Load(); n != 1 {
		t.Errorf("%d stat calls for 100 parallel reads, want 1", n)
	}
}

func TestOwnUpdateIsNotReloaded(t *testing.T) {
	s, _, c, _ := clockedStore(t, `{"files":{}}`)
	logs := captureLogs(t)

	if err := s.Update(addShare("docs")); err != nil {
		t.Fatal(err)
	}
	c.advance(checkInterval)
	s.Current()

	if n := count(*logs, "reloaded"); n != 0 {
		t.Errorf("own update was reloaded %d times", n)
	}
}

func TestUpdateKeepsAHandEditWithinTheInterval(t *testing.T) {
	s, path, _, _ := clockedStore(t, `{"files":{}}`)
	handEdit(t, path, `{"files":{"hand":{"path":"/srv/hand"}}}`)

	if err := s.Update(addShare("admin")); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		name string
		n    int
	}{{"memory", s.Current().ShareCount()}, {"disk", reopened.Current().ShareCount()}} {
		if v.n != 2 {
			t.Errorf("%s: %d shares, want the hand edit and the update", v.name, v.n)
		}
	}
}

func TestReplacedFileWithSameSizeAndTimeIsDetected(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{"aaaa":{}}}`)
	old, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(`{"files":{"bbbb":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}

	c.advance(checkInterval)
	if _, ok := s.Current().Share("bbbb"); !ok {
		t.Error("file replaced with same size and mtime was not picked up")
	}
}

func TestReadersUpdatesAndChecksRunTogether(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if err := s.Update(addShare(fmt.Sprintf("s%02d", i))); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			c.advance(checkInterval)
			for range s.Current().Shares() {
			}
		})
	}
	wg.Wait()

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := reopened.Current().ShareCount(); n != 20 {
		t.Errorf("on disk: %d shares, want 20", n)
	}
	if n := s.Current().ShareCount(); n != 20 {
		t.Errorf("in memory: %d shares, want 20", n)
	}
}

func recordChanges(s *FileStore) *[][2]int {
	var mu sync.Mutex
	got := &[][2]int{}
	s.OnOutsideChange(func(old, new config.View) {
		mu.Lock()
		*got = append(*got, [2]int{old.ShareCount(), new.ShareCount()})
		mu.Unlock()
	})
	return got
}

func TestOutsideChangeIsReportedWithOldAndNewState(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	got := recordChanges(s)
	handEdit(t, path, `{"files":{"hand":{"path":"/srv/hand"}}}`)

	c.advance(checkInterval)
	s.Current()

	if len(*got) != 1 || (*got)[0] != [2]int{0, 1} {
		t.Errorf("reported changes = %v, want one from 0 to 1 shares", *got)
	}
}

func TestOwnUpdateIsNotReportedAsOutsideChange(t *testing.T) {
	s, _, c, _ := clockedStore(t, `{"files":{}}`)
	got := recordChanges(s)

	if err := s.Update(addShare("docs")); err != nil {
		t.Fatal(err)
	}
	c.advance(checkInterval)
	s.Current()

	if len(*got) != 0 {
		t.Errorf("own update reported as outside change: %v", *got)
	}
}

func TestUpdateReportsTheOutsideChangeItFoundFirst(t *testing.T) {
	s, path, _, _ := clockedStore(t, `{"files":{}}`)
	got := recordChanges(s)
	handEdit(t, path, `{"files":{"hand":{"path":"/srv/hand"}}}`)

	if err := s.Update(addShare("admin")); err != nil {
		t.Fatal(err)
	}

	if len(*got) != 1 || (*got)[0] != [2]int{0, 1} {
		t.Errorf("reported changes = %v, want the hand edit only", *got)
	}
}

func TestListenersMayReadAndWriteTheStore(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	s.OnOutsideChange(func(old, new config.View) {
		s.Current()
		s.Update(addShare("from-listener"))
	})
	handEdit(t, path, `{"files":{"hand":{"path":"/srv/hand"}}}`)

	done := make(chan struct{})
	go func() {
		c.advance(checkInterval)
		s.Current()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a listener using the store deadlocked")
	}
	if _, ok := s.Current().Share("from-listener"); !ok {
		t.Error("update from the listener got lost")
	}
}

func TestHandEditsDuringUpdatesAndReadsAreRaceFree(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	recordChanges(s)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { s.Update(addShare(fmt.Sprintf("s%02d", i))) })
		wg.Go(func() {
			c.advance(checkInterval)
			s.Current()
		})
		wg.Go(func() { os.WriteFile(path, fmt.Appendf(nil, `{"files":{"h%02d":{}}}`, i), 0o600) })
	}
	wg.Wait()
}

func TestTouchedButUnchangedFileIsNotReloaded(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	got := recordChanges(s)
	logs := captureLogs(t)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	c.advance(checkInterval)
	s.Current()

	if len(*got) != 0 || count(*logs, "reloaded") != 0 {
		t.Errorf("a touched but unchanged file was reloaded: changes %v", *got)
	}
}

func TestOwnSavesCountTheRevisionUp(t *testing.T) {
	s, path, _, _ := clockedStore(t, `{"files":{}}`)
	s.Update(addShare("a"))
	s.Update(addShare("b"))

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.cur.Load().Revision; got != 2 {
		t.Errorf("revision on disk = %d after two updates, want 2", got)
	}
}

func TestHandEditOnTheCurrentRevisionIsTakenOver(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	if err := s.Update(addShare("docs")); err != nil {
		t.Fatal(err)
	}
	handEdit(t, path, `{"revision":1,"files":{"docs":{"path":"/srv/docs"},"hand":{"path":"/srv/hand"}}}`)

	c.advance(checkInterval)
	if _, ok := s.Current().Share("hand"); !ok {
		t.Error("hand edit on the current revision was not taken over")
	}
}

func asideFiles(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".rejected-*")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestBrokenFileKeepsTheLastGoodStateAndIsSetAsideOnUpdate(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{"docs":{"path":"/srv/docs"}}}`)
	logs := captureLogs(t)
	handEdit(t, path, `{"files":`)

	for range 3 {
		c.advance(checkInterval)
		if _, ok := s.Current().Share("docs"); !ok {
			t.Fatal("last good state lost")
		}
	}
	if n := count(*logs, "not taken over"); n != 1 {
		t.Errorf("%d not-taken-over log lines over three checks, want 1", n)
	}

	if err := s.Update(addShare("new")); err != nil {
		t.Fatalf("update on a broken file: %v", err)
	}
	aside := asideFiles(t, path)
	if len(aside) != 1 {
		t.Fatalf("set-aside files = %v, want one", aside)
	}
	if data, _ := os.ReadFile(aside[0]); string(data) != `{"files":` {
		t.Errorf("set-aside file = %q, want the broken content", data)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("data.json after the update: %v", err)
	}
	if reopened.Current().ShareCount() != 2 {
		t.Errorf("data.json has %d shares, want the last good state plus the update", reopened.Current().ShareCount())
	}
}

func TestFailedSaveLeavesTheBrokenFileInPlace(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{"docs":{"path":"/srv/docs"}}}`)
	handEdit(t, path, `{"files":`)
	c.advance(checkInterval)
	s.Current()
	s.createTemp = func(string, string) (*os.File, error) { return nil, fmt.Errorf("disk full") }

	if err := s.Update(addShare("new")); err == nil {
		t.Fatal("update succeeded although the temp file could not be created")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != `{"files":` {
		t.Errorf("data.json = %q, %v; want the broken content untouched", data, err)
	}
	if aside := asideFiles(t, path); len(aside) != 0 {
		t.Errorf("set-aside files = %v, want none", aside)
	}
	if _, ok := s.Current().Share("new"); ok {
		t.Error("failed update became current")
	}
}

func TestFailedRenameRestoresTheRefusedFile(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{"docs":{"path":"/srv/docs"}}}`)
	handEdit(t, path, `{"files":`)
	c.advance(checkInterval)
	s.Current()
	s.rename = func(from, to string) error {
		if strings.Contains(from, ".data.json.tmp") {
			return fmt.Errorf("i/o error")
		}
		return os.Rename(from, to)
	}

	if err := s.Update(addShare("new")); err == nil {
		t.Fatal("update succeeded although the rename failed")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != `{"files":` {
		t.Errorf("data.json = %q, %v; want the refused content back in place", data, err)
	}
	if aside := asideFiles(t, path); len(aside) != 0 {
		t.Errorf("set-aside files = %v, want none", aside)
	}
	if tmp, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".data.json.tmp*")); len(tmp) != 0 {
		t.Errorf("temp files left: %v", tmp)
	}
}

func TestSetAsideKeepsPermissions(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	handEdit(t, path, `{"files":`)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	c.advance(checkInterval)
	s.Current()
	if err := s.Update(addShare("new")); err != nil {
		t.Fatal(err)
	}
	aside := asideFiles(t, path)
	if len(aside) != 1 {
		t.Fatalf("set-aside files = %v, want one", aside)
	}
	if fi, err := os.Stat(aside[0]); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("set-aside file mode = %v, %v; want 0640 like data.json", fi.Mode().Perm(), err)
	}
}

func TestSetAsideLeavesTheTargetOfASymlinkedFile(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	target := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(target, []byte(`{"files":`), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	c.advance(checkInterval)
	s.Current()
	if err := s.Update(addShare("new")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != `{"files":` {
		t.Errorf("symlink target = %q, want it untouched", data)
	}
	aside := asideFiles(t, path)
	if len(aside) != 1 {
		t.Fatalf("set-aside files = %v, want one", aside)
	}
	if link, err := os.Readlink(aside[0]); err != nil || link != target {
		t.Errorf("set-aside entry = %q, %v; want the symlink to %s", link, err, target)
	}
}

func TestUpdateThatChangesNothingWritesNothing(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"revision":3,"files":{}}`)
	before, _ := os.Stat(path)
	if err := s.Update(func(*config.Config) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(path); !sameFile(before, after) {
		t.Error("an update without changes rewrote data.json")
	}

	handEdit(t, path, `{"files":`)
	c.advance(checkInterval)
	s.Current()
	if err := s.Update(func(*config.Config) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != `{"files":` {
		t.Errorf("data.json = %q, want the refused file untouched", data)
	}
	if aside := asideFiles(t, path); len(aside) != 0 {
		t.Errorf("set-aside files = %v, want none", aside)
	}
	if s.Refused() == "" || s.lastRevision != 3 {
		t.Errorf("Refused() = %q, revision = %d; want the refusal kept and revision 3", s.Refused(), s.lastRevision)
	}
}

func TestUpdateIfAcceptedLeavesARefusedFileAlone(t *testing.T) {
	s, path, _, _ := clockedStore(t, `{"revision":3,"files":{}}`)
	handEdit(t, path, `{"files":`)

	err := s.UpdateIfAccepted(addShare("new"))
	if !errors.Is(err, config.ErrRefused) {
		t.Fatalf("UpdateIfAccepted on a refused file = %v, want ErrRefused", err)
	}
	if data, _ := os.ReadFile(path); string(data) != `{"files":` {
		t.Errorf("data.json = %q, want the refused file untouched", data)
	}
	if aside := asideFiles(t, path); len(aside) != 0 {
		t.Errorf("set-aside files = %v, want none", aside)
	}
	if _, ok := s.Current().Share("new"); ok || s.Refused() == "" || s.lastRevision != 3 {
		t.Errorf("share applied = %v, Refused() = %q, revision = %d; want nothing applied, refusal kept, revision 3", ok, s.Refused(), s.lastRevision)
	}
}

func TestUpdateIfAcceptedSavesWhileTheFileIsAccepted(t *testing.T) {
	s, path, _, _ := clockedStore(t, `{"files":{}}`)
	if err := s.UpdateIfAccepted(addShare("new")); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Current().Share("new"); !ok {
		t.Error("UpdateIfAccepted did not save")
	}
}

func TestDeletedFileKeepsTheLastGoodStateAndIsRecreatedOnUpdate(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{"docs":{"path":"/srv/docs"}}}`)
	logs := captureLogs(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		c.advance(checkInterval)
		if _, ok := s.Current().Share("docs"); !ok {
			t.Fatal("last good state lost")
		}
	}
	if n := count(*logs, "not taken over"); n != 1 {
		t.Errorf("%d not-taken-over log lines over three checks, want 1", n)
	}
	if err := s.Update(addShare("new")); err != nil {
		t.Fatalf("update without a file: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("data.json not recreated: %v", err)
	}
	if reopened.Current().ShareCount() != 2 {
		t.Errorf("recreated data.json has %d shares, want 2", reopened.Current().ShareCount())
	}
}

func TestStaleEditorBufferIsNotTakenOver(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{"docs":{"path":"/srv/docs","password":"old-hash"}}}`)
	got := recordChanges(s)
	logs := captureLogs(t)
	buffer, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	setPassword := func(hash string) func(*config.Config) error {
		return func(c *config.Config) error {
			fd := c.Files["docs"]
			fd.Password = hash
			c.Files["docs"] = fd
			return nil
		}
	}
	if err := s.Update(setPassword("new-hash")); err != nil {
		t.Fatal(err)
	}

	handEdit(t, path, string(buffer))
	c.advance(checkInterval)

	if fd, _ := s.Current().Share("docs"); fd.Password != "new-hash" {
		t.Errorf("password = %q, a stale editor buffer brought the old one back", fd.Password)
	}
	if len(*got) != 0 {
		t.Errorf("stale buffer reported as change: %v", *got)
	}
	if count(*logs, "older than the server's last save") != 1 {
		t.Errorf("logs = %q, want one line about the stale file", *logs)
	}

	if err := s.Update(setPassword("newer-hash")); err != nil {
		t.Fatalf("revoking through the server over a stale file: %v", err)
	}
	if aside := asideFiles(t, path); len(aside) != 1 {
		t.Errorf("set-aside files = %v, want the stale buffer kept", aside)
	}
	reopened, _ := Open(path)
	if fd, _ := reopened.Current().Share("docs"); fd.Password != "newer-hash" || reopened.cur.Load().Revision != 2 {
		t.Errorf("data.json: password %q revision %d, want newer-hash at revision 2", fd.Password, reopened.cur.Load().Revision)
	}
}

func TestRestoredBackupWithRaisedRevisionIsTakenOver(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{"old":{"path":"/srv/old"}}}`)
	backup, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Update(addShare("a"))
	s.Update(addShare("b"))

	handEdit(t, path, string(backup))
	c.advance(checkInterval)
	if s.Current().ShareCount() != 3 {
		t.Fatal("restored backup with an old revision was taken over")
	}

	handEdit(t, path, `{"revision":2,"files":{"old":{"path":"/srv/old"}}}`)
	c.advance(checkInterval)
	if n := s.Current().ShareCount(); n != 1 {
		t.Errorf("%d shares, want the restored backup once its revision is raised", n)
	}
}

func TestRestartTakesTheFileOnDiskEvenIfStale(t *testing.T) {
	s, path, _, _ := clockedStore(t, `{"files":{}}`)
	stale, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Update(addShare("a"))
	handEdit(t, path, string(stale))

	restarted, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := restarted.Current().ShareCount(); n != 0 {
		t.Errorf("after restart %d shares, want the file on disk", n)
	}
}

func TestRefusedNamesTheReasonUntilResolved(t *testing.T) {
	s, path, c, _ := clockedStore(t, `{"files":{}}`)
	if r := s.Refused(); r != "" {
		t.Fatalf("Refused = %q on a good file", r)
	}
	handEdit(t, path, `{"files":`)
	c.advance(checkInterval)
	s.Current()
	if r := s.Refused(); !strings.Contains(r, "decode") {
		t.Errorf("Refused = %q, want the decode error", r)
	}
	if err := s.Update(addShare("docs")); err != nil {
		t.Fatal(err)
	}
	if r := s.Refused(); r != "" {
		t.Errorf("Refused = %q after the server wrote its state", r)
	}
}
