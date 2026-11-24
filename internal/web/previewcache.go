package web

import (
	"container/list"
	"sync"
	"time"
)

const (
	previewCacheMax = 64 << 20
	previewCacheTTL = 30 * time.Minute
)

type previewKey struct {
	path string
	size int64
	mod  time.Time
	edge int
}

type previewItem struct {
	key  previewKey
	jpeg []byte
	used time.Time
}

// previewCache keeps encoded preview JPEGs in memory, at most previewCacheMax bytes and each until it was not used
// for previewCacheTTL; when full the least recently used go first.
type previewCache struct {
	mu       sync.Mutex
	items    map[previewKey]*list.Element
	order    list.List
	bytes    int
	sweeping bool
}

// load returns the preview stored under k; ok is false when there is none.
func (c *previewCache) load(k previewKey) (jpeg []byte, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	item := el.Value.(*previewItem)
	item.used = time.Now()
	c.order.MoveToFront(el)
	return item.jpeg, true
}

// store keeps the preview jpeg under k.
func (c *previewCache) store(k previewKey, jpeg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.remove(el)
	}
	if c.items == nil {
		c.items = make(map[previewKey]*list.Element)
	}
	c.items[k] = c.order.PushFront(&previewItem{key: k, jpeg: jpeg, used: time.Now()})
	c.bytes += len(jpeg)
	for c.bytes > previewCacheMax {
		c.remove(c.order.Back())
	}
	if !c.sweeping {
		c.sweeping = true
		time.AfterFunc(previewCacheTTL, c.sweep)
	}
}

// sweep drops the previews not used for previewCacheTTL and runs again when the next one is due, while any is left.
func (c *previewCache) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.order.Back(); el != nil; el = c.order.Back() {
		if left := previewCacheTTL - time.Since(el.Value.(*previewItem).used); left > 0 {
			time.AfterFunc(left, c.sweep)
			return
		}
		c.remove(el)
	}
	c.sweeping = false
}

// remove drops the entry el.
func (c *previewCache) remove(el *list.Element) {
	item := c.order.Remove(el).(*previewItem)
	delete(c.items, item.key)
	c.bytes -= len(item.jpeg)
}
