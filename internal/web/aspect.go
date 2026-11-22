package web

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Wirezat/fileshare/internal/filetype"
	"github.com/Wirezat/fileshare/internal/media"
)

const (
	aspectCacheMax = 100_000
	aspectWorkers  = 8
	aspectBudget   = time.Second
)

type aspectEntry struct {
	size  int64
	mod   time.Time
	ratio float64
}

type aspectCache struct {
	mu sync.Mutex
	m  map[string]aspectEntry
}

// cached returns the stored aspect ratio of the file at path while its size and mod time still match.
func (c *aspectCache) cached(path string, size int64, mod time.Time) (ratio float64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[path]
	return e.ratio, ok && e.size == size && e.mod.Equal(mod)
}

// read returns the aspect ratio from the header of the file at path, 0 when it has none, and stores it
// for size and mod time.
func (c *aspectCache) read(path string, size int64, mod time.Time) float64 {
	var ratio float64
	if f, err := os.Open(path); err == nil {
		if r, ok := media.AspectRatio(f, path); ok {
			ratio = math.Round(r*1e4) / 1e4
		}
		f.Close()
	}
	c.mu.Lock()
	if c.m == nil || len(c.m) >= aspectCacheMax {
		c.m = make(map[string]aspectEntry)
	}
	c.m[path] = aspectEntry{size: size, mod: mod, ratio: ratio}
	c.mu.Unlock()
	return ratio
}

// addAspectRatios sets the ratio of each image and video entry of the directory dir; files are the infos the
// entries were built from, in the same order. Headers not in the cache are read by aspectWorkers goroutines;
// entries still unread after aspectBudget keep no ratio while their reading finishes into the cache.
func (s *Server) addAspectRatios(entries []ListEntry, files []media.FileInfo, dir string) {
	var todo []int
	for i := range entries {
		switch entries[i].View {
		case filetype.ViewImage, filetype.ViewConverted, filetype.ViewVideo:
		default:
			continue
		}
		if r, ok := s.aspects.cached(filepath.Join(dir, files[i].Name), files[i].Size, files[i].ModTime); ok {
			entries[i].Ratio = r
		} else {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return
	}
	jobs := make(chan int, len(todo))
	for _, i := range todo {
		jobs <- i
	}
	close(jobs)
	type result struct {
		index int
		ratio float64
	}
	results := make(chan result, len(todo))
	for range min(aspectWorkers, len(todo)) {
		go func() {
			for i := range jobs {
				results <- result{i, s.aspects.read(filepath.Join(dir, files[i].Name), files[i].Size, files[i].ModTime)}
			}
		}()
	}
	deadline := time.After(aspectBudget)
	for range todo {
		select {
		case r := <-results:
			entries[r.index].Ratio = r.ratio
		case <-deadline:
			return
		}
	}
}
