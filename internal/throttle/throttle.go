package throttle

import (
	"context"
	"net/http"
	"sync"
	"time"
)

const (
	chunk        = 64 << 10
	bytesPerMbit = 125000
	pace         = 20 * time.Millisecond
)

var buffers = sync.Pool{New: func() any { b := make([]byte, 0, chunk); return &b }}

// Limiter caps the combined rate of every response it paces at the rate its source reports.
type Limiter struct {
	source  func() (enabled bool, mbit int)
	mu      sync.Mutex
	tokens  float64
	last    time.Time
	waiting map[string]int
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
}

// New returns a limiter that asks source for the current setting whenever a response waits for bytes.
func New(source func() (enabled bool, mbit int)) *Limiter {
	return &Limiter{source: source, tokens: chunk, waiting: map[string]int{}, now: time.Now, sleep: sleep}
}

// Handler paces next's response in chunks of up to 64 KiB at the current rate, shared fairly per client(r); HEAD passes unpaced.
func (l *Limiter) Handler(next http.Handler, client func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		tw := &writer{ResponseWriter: w, l: l, ctx: r.Context()}
		if client != nil {
			tw.client = client(r)
		}
		defer tw.release()
		next.ServeHTTP(tw, r)
		tw.emit()
	})
}

// take blocks until n bytes for client may pass or ctx ends; waiting clients share each pace evenly, then per response.
func (l *Limiter) take(ctx context.Context, client string, n int) error {
	missing := float64(n)
	waiting := false
	defer func() {
		if waiting {
			l.mu.Lock()
			if l.waiting[client]--; l.waiting[client] == 0 {
				delete(l.waiting, client)
			}
			l.mu.Unlock()
		}
	}()
	for {
		on, mbit := l.source()
		if !on {
			return nil
		}
		rate := float64(mbit * bytesPerMbit)
		l.mu.Lock()
		now := l.now()
		l.tokens = min(capacity(rate), l.tokens+now.Sub(l.last).Seconds()*rate)
		l.last = now
		clients, mine := len(l.waiting), l.waiting[client]
		if !waiting {
			if mine == 0 {
				clients++
			}
			mine++
		}
		share := capacity(rate)
		if clients > 1 || mine > 1 {
			share = rate * pace.Seconds() / float64(clients) / float64(mine)
		}
		got := min(l.tokens, missing, share)
		l.tokens -= got
		missing -= got
		if missing > 0 && !waiting {
			waiting = true
			l.waiting[client]++
		}
		l.mu.Unlock()
		if missing <= 0 {
			return nil
		}
		if err := l.sleep(ctx, pace); err != nil {
			l.mu.Lock()
			l.tokens = min(capacity(rate), l.tokens+float64(n)-missing)
			l.mu.Unlock()
			return err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// capacity returns how many bytes the bucket holds at rate bytes per second: one chunk, or one pace worth of rate if that is more.
func capacity(rate float64) float64 {
	return max(chunk, rate*pace.Seconds())
}

type writer struct {
	http.ResponseWriter
	l      *Limiter
	ctx    context.Context
	client string
	buf    *[]byte
	err    error
	sent   bool
}

func (w *writer) WriteHeader(code int) {
	if !w.sent {
		w.sent = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *writer) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.WriteHeader(http.StatusOK)
	}
	written := 0
	for len(p) > 0 && w.err == nil {
		if w.buf == nil {
			w.buf = buffers.Get().(*[]byte)
		}
		b := *w.buf
		n := copy(b[len(b):cap(b)], p)
		*w.buf = b[:len(b)+n]
		written += n
		p = p[n:]
		if len(*w.buf) == chunk {
			w.emit()
		}
	}
	return written, w.err
}

func (w *writer) emit() {
	if w.buf == nil {
		return
	}
	if len(*w.buf) > 0 && w.err == nil {
		if w.err = w.l.take(w.ctx, w.client, len(*w.buf)); w.err == nil {
			_, w.err = w.ResponseWriter.Write(*w.buf)
		}
	}
	*w.buf = (*w.buf)[:0]
}

func (w *writer) release() {
	if w.buf != nil {
		*w.buf = (*w.buf)[:0]
		buffers.Put(w.buf)
		w.buf = nil
	}
}

func (w *writer) Flush() {
	w.emit()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
