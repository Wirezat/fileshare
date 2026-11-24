package web

import (
	"testing"
	"testing/synctest"
	"time"
)

func cacheKey(n int64) previewKey {
	return previewKey{path: "f", size: n}
}

func TestPreviewCacheDropsTheLeastRecentlyUsedWhenFull(t *testing.T) {
	var c previewCache
	third := make([]byte, previewCacheMax/3)
	for i := range int64(3) {
		c.store(cacheKey(i), third)
	}
	c.load(cacheKey(0))
	c.store(cacheKey(3), third)

	for n, want := range map[int64]bool{0: true, 1: false, 2: true, 3: true} {
		if _, ok := c.load(cacheKey(n)); ok != want {
			t.Errorf("preview %d cached = %v, want %v", n, ok, want)
		}
	}
	if c.bytes > previewCacheMax {
		t.Errorf("cache holds %d bytes, limit %d", c.bytes, previewCacheMax)
	}
}

func TestPreviewCacheFreesWhatWasNotUsedForItsTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var c previewCache
		c.store(cacheKey(1), make([]byte, 10))
		c.store(cacheKey(2), make([]byte, 10))
		time.Sleep(previewCacheTTL - time.Minute)
		c.load(cacheKey(2))
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if c.bytes != 10 {
			t.Errorf("%d bytes left, want 10: only the used preview", c.bytes)
		}
		time.Sleep(previewCacheTTL)
		synctest.Wait()
		if c.bytes != 0 || len(c.items) != 0 {
			t.Errorf("%d bytes and %d previews left, want none", c.bytes, len(c.items))
		}
	})
}
