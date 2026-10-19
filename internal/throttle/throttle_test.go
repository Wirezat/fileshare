package throttle

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type setting struct {
	on   atomic.Bool
	mbit atomic.Int64
}

func (s *setting) get() (bool, int) { return s.on.Load(), int(s.mbit.Load()) }

func newSetting(on bool, mbit int) *setting {
	s := &setting{}
	s.on.Store(on)
	s.mbit.Store(int64(mbit))
	return s
}

type hookWriter struct {
	*httptest.ResponseRecorder
	afterWrite func(total int)
	total      int
	calls      int
}

func (h *hookWriter) Write(p []byte) (int, error) {
	n, err := h.ResponseRecorder.Write(p)
	h.total += n
	h.calls++
	if h.afterWrite != nil {
		h.afterWrite(h.total)
	}
	return n, err
}

func serve(ctx context.Context, l *Limiter, w http.ResponseWriter, body func(http.ResponseWriter)) time.Duration {
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	start := time.Now()
	l.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { body(w) })).ServeHTTP(w, r)
	return time.Since(start)
}

func writeAll(n int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.Write(make([]byte, n)) }
}

func TestActiveLimitPacesTheResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	d := serve(context.Background(), New(newSetting(true, 8).get), rec, writeAll(chunk+1_000_000))
	if d < 900*time.Millisecond || d > 2*time.Second {
		t.Errorf("1 MB beyond the burst at 8 Mbit/s took %v, want about 1 s", d)
	}
	if rec.Body.Len() != chunk+1_000_000 {
		t.Errorf("body has %d bytes", rec.Body.Len())
	}
}

func TestLimitOffDoesNotWait(t *testing.T) {
	if d := serve(context.Background(), New(newSetting(false, 1).get), httptest.NewRecorder(), writeAll(5_000_000)); d > 2*time.Second {
		t.Errorf("5 MB with the limit off took %v", d)
	}
}

func TestSwitchingOnSlowsARunningResponse(t *testing.T) {
	s := newSetting(false, 8)
	h := &hookWriter{ResponseRecorder: httptest.NewRecorder()}
	h.afterWrite = func(total int) {
		if total >= 4*chunk {
			s.on.Store(true)
		}
	}
	if d := serve(context.Background(), New(s.get), h, writeAll(4*chunk+chunk+1_000_000)); d < 900*time.Millisecond {
		t.Errorf("response switched on after 256 KiB took %v, want about 1 s for the rest", d)
	}
}

func TestSwitchingOffSpeedsUpARunningResponse(t *testing.T) {
	s := newSetting(true, 1)
	h := &hookWriter{ResponseRecorder: httptest.NewRecorder()}
	h.afterWrite = func(total int) {
		if total >= chunk {
			s.on.Store(false)
		}
	}
	if d := serve(context.Background(), New(s.get), h, writeAll(80*chunk)); d > 2*time.Second {
		t.Errorf("response switched off after the first chunk took %v, want it to finish at once", d)
	}
}

func TestChangedRateAppliesToARunningResponse(t *testing.T) {
	s := newSetting(true, 1)
	h := &hookWriter{ResponseRecorder: httptest.NewRecorder()}
	h.afterWrite = func(total int) {
		if total >= chunk {
			s.mbit.Store(80)
		}
	}
	if d := serve(context.Background(), New(s.get), h, writeAll(chunk+5_000_000)); d > 3*time.Second {
		t.Errorf("5 MB after raising 1 to 80 Mbit/s took %v, want about 0.5 s", d)
	}
}

func TestResponsesShareOneLimit(t *testing.T) {
	l := New(newSetting(true, 8).get)
	start := time.Now()
	done := make(chan struct{})
	for range 2 {
		go func() {
			serve(context.Background(), l, httptest.NewRecorder(), writeAll(500_000))
			done <- struct{}{}
		}()
	}
	<-done
	<-done
	if d := time.Since(start); d < 800*time.Millisecond {
		t.Errorf("two responses of 500 KB at 8 Mbit/s finished in %v, want about 0.9 s together", d)
	}
}

func TestSmallWritesAreSentAsOneChunk(t *testing.T) {
	h := &hookWriter{ResponseRecorder: httptest.NewRecorder()}
	serve(context.Background(), New(newSetting(true, 8).get), h, func(w http.ResponseWriter) {
		for i := range 1000 {
			w.Write([]byte{byte(i)})
		}
	})
	if h.calls != 1 || h.Body.Len() != 1000 || h.Body.Bytes()[999] != byte(999%256) {
		t.Errorf("1000 one-byte writes reached the client as %d writes with %d bytes, want one write with all bytes in order", h.calls, h.Body.Len())
	}
}

func TestSmallWritesBesideARunningDownloadWaitAboutOneChunk(t *testing.T) {
	l := New(newSetting(true, 8).get)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serve(ctx, l, httptest.NewRecorder(), writeAll(10_000_000))
	time.Sleep(200 * time.Millisecond)

	rec := httptest.NewRecorder()
	d := serve(context.Background(), l, rec, func(w http.ResponseWriter) {
		for range 100 {
			w.Write(make([]byte, 600))
		}
	})
	if d > 400*time.Millisecond {
		t.Errorf("100 small writes beside a running download took %v, want about one chunk (65 ms)", d)
	}
	if rec.Body.Len() != 100*600 {
		t.Errorf("body has %d bytes", rec.Body.Len())
	}
}

func TestCanceledContextEndsTheWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	var err, later error
	d := serve(ctx, New(newSetting(true, 1).get), httptest.NewRecorder(), func(w http.ResponseWriter) {
		_, err = w.Write(make([]byte, 2_000_000))
		_, later = w.Write([]byte("more"))
	})
	if !errors.Is(err, context.Canceled) || !errors.Is(later, context.Canceled) {
		t.Errorf("writes = %v, %v; want context.Canceled for both", err, later)
	}
	if d > time.Second {
		t.Errorf("canceled response returned after %v", d)
	}
}

func TestFlushSendsTheBufferAndTheOriginalWriterStaysHidden(t *testing.T) {
	rec := httptest.NewRecorder()
	serve(context.Background(), New(newSetting(false, 1).get), rec, func(w http.ResponseWriter) {
		w.Write([]byte("head"))
		w.(http.Flusher).Flush()
		if !rec.Flushed || !bytes.Equal(rec.Body.Bytes(), []byte("head")) {
			t.Errorf("after Flush: flushed %v, body %q", rec.Flushed, rec.Body.String())
		}
		if _, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			t.Error("the original writer can be reached around the throttle")
		}
	})
}

func startDownloads(t *testing.T, l *Limiter, n, size int) (wait func() time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{}, n)
	for range n {
		go func() {
			serve(ctx, l, httptest.NewRecorder(), writeAll(size))
			done <- struct{}{}
		}()
	}
	time.Sleep(500 * time.Millisecond)
	return func() time.Duration {
		start := time.Now()
		for range n {
			<-done
		}
		return time.Since(start)
	}
}

func TestSwitchingOffReachesEveryWaitingResponse(t *testing.T) {
	s := newSetting(true, 1)
	wait := startDownloads(t, New(s.get), 10, 200_000)
	s.on.Store(false)
	if d := wait(); d > 300*time.Millisecond {
		t.Errorf("ten downloads finished %v after the limit was switched off, want at once", d)
	}
}

func TestRateChangeReachesEveryWaitingResponse(t *testing.T) {
	s := newSetting(true, 1)
	wait := startDownloads(t, New(s.get), 10, 1_000_000)
	s.mbit.Store(80)
	if d := wait(); d > 2*time.Second {
		t.Errorf("ten downloads of 1 MB took %v after raising 1 to 80 Mbit/s, want about 1 s", d)
	}
}

func TestSmallResponseBesideTenDownloadsGetsItsShare(t *testing.T) {
	l := New(newSetting(true, 1).get)
	startDownloads(t, l, 10, 2_000_000)
	rec := httptest.NewRecorder()
	if d := serve(context.Background(), l, rec, writeAll(6000)); d > 1200*time.Millisecond {
		t.Errorf("a 6 KB page beside ten downloads at 1 Mbit/s took %v, want about its fair share of 0.5 s", d)
	}
	if rec.Body.Len() != 6000 {
		t.Errorf("body has %d bytes", rec.Body.Len())
	}
}

func TestSwitchingOffWakesAResponseWaitingForAWholeChunk(t *testing.T) {
	s := newSetting(true, 1)
	l := New(s.get)
	serve(context.Background(), l, httptest.NewRecorder(), writeAll(chunk))
	done := make(chan struct{})
	go func() {
		serve(context.Background(), l, httptest.NewRecorder(), writeAll(chunk))
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	s.on.Store(false)
	<-done
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Errorf("a response waiting about 0.5 s for its chunk finished %v after the limit was switched off, want at once", d)
	}
}

func TestDownloadsProgressBesideAFloodOfSmallResponses(t *testing.T) {
	l := New(newSetting(true, 8).get)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range 3 {
		go func() {
			for ctx.Err() == nil {
				serve(ctx, l, httptest.NewRecorder(), writeAll(2000))
			}
		}()
	}

	done := make(chan struct{}, 10)
	for range 10 {
		go func() {
			serve(ctx, l, httptest.NewRecorder(), writeAll(100_000))
			done <- struct{}{}
		}()
	}
	timeout := time.After(8 * time.Second)
	for i := range 10 {
		select {
		case <-done:
		case <-timeout:
			t.Fatalf("only %d of 10 downloads of 100 KB finished within 8 s beside a flood of small responses at 8 Mbit/s", i)
		}
	}
}

func TestCanceledResponseGivesItsTokensBack(t *testing.T) {
	l := New(newSetting(true, 1).get)
	serve(context.Background(), l, httptest.NewRecorder(), writeAll(chunk))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	serve(ctx, l, httptest.NewRecorder(), writeAll(chunk))

	if d := serve(context.Background(), l, httptest.NewRecorder(), writeAll(30_000)); d > 120*time.Millisecond {
		t.Errorf("30 KB right after a canceled response had collected about 37 KB took %v, want at once", d)
	}
}

func TestHighRateIsReached(t *testing.T) {
	d := serve(context.Background(), New(newSetting(true, 400).get), httptest.NewRecorder(), writeAll(10_000_000))
	if d < 150*time.Millisecond || d > 2500*time.Millisecond {
		t.Errorf("10 MB at 400 Mbit/s took %v, want about 0.2 s", d)
	}
}

type fakePace struct {
	mu    sync.Mutex
	now   time.Time
	queue []chan struct{}
}

func (f *fakePace) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakePace) sleep(ctx context.Context, _ time.Duration) error {
	ch := make(chan struct{})
	f.mu.Lock()
	f.queue = append(f.queue, ch)
	f.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakePace) asleep() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queue)
}

func (f *fakePace) wakeFirst() {
	f.mu.Lock()
	ch := f.queue[0]
	f.queue = f.queue[1:]
	f.mu.Unlock()
	close(ch)
}

func TestAWaiterThatAlwaysWakesSecondGetsItsShare(t *testing.T) {
	f := &fakePace{now: time.Unix(1000, 0)}
	l := New(newSetting(true, 8).get)
	l.now, l.sleep = f.clock, f.sleep
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitAsleep := func(n int, done <-chan struct{}) bool {
		for f.asleep() < n {
			select {
			case <-done:
				return true
			case <-time.After(time.Millisecond):
			}
		}
		return false
	}

	go l.take(ctx, 100_000_000)
	waitAsleep(1, nil)
	small := make(chan struct{})
	go func() {
		l.take(ctx, 60_000)
		close(small)
	}()
	waitAsleep(2, nil)

	for step := 1; step <= 40; step++ {
		f.mu.Lock()
		f.now = f.now.Add(pace)
		f.mu.Unlock()
		f.wakeFirst()
		waitAsleep(2, nil)
		f.wakeFirst()
		if waitAsleep(2, small) {
			if step > 8 {
				t.Errorf("60 KB that always woke second beside one download at 8 Mbit/s took %d steps of %v, want about 6", step, pace)
			}
			return
		}
	}
	t.Fatalf("60 KB that always woke second beside one download at 8 Mbit/s got nothing in 40 steps of %v", pace)
}

func TestManyConnectionsOfOneClientEachGetOneShare(t *testing.T) {
	l := New(newSetting(true, 8).get)
	startDownloads(t, l, 20, 100_000_000)
	d := serve(context.Background(), l, httptest.NewRecorder(), writeAll(20_000))
	if d < 200*time.Millisecond || d > 1200*time.Millisecond {
		t.Errorf("a 20 KB page beside 20 downloads at 8 Mbit/s took %v, want about one share in 21 (0.42 s)", d)
	}
}

func TestHeadIsNotPaced(t *testing.T) {
	l := New(newSetting(true, 1).get)
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(make([]byte, 200_000)) }))
	start := time.Now()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodHead, "/", nil))
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("HEAD whose handler writes 200 KB took %v at 1 Mbit/s, want no wait", d)
	}
	if d := serve(context.Background(), l, httptest.NewRecorder(), writeAll(chunk)); d > 100*time.Millisecond {
		t.Errorf("a full chunk right after the HEAD waited %v, want the bucket untouched", d)
	}
}

func TestStatusAndHeadersAreFixedByTheFirstWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	serve(context.Background(), New(newSetting(true, 8).get), rec, func(w http.ResponseWriter) {
		w.Write([]byte("partial"))
		w.Header().Set("X-Late", "1")
		w.WriteHeader(http.StatusInternalServerError)
	})
	res := rec.Result()
	if res.StatusCode != http.StatusOK || res.Header.Get("X-Late") != "" {
		t.Errorf("status %d, X-Late %q after writing first; want 200 and no late header, as without the throttle", res.StatusCode, res.Header.Get("X-Late"))
	}
}
