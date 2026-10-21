package stats

import (
	"time"
)

const (
	minSpeedBytes    = 1 << 20
	minSpeedDuration = time.Second
)

// Download describes one response that hands out file content as a download.
type Download struct {
	Share     string
	File      string
	Zip       bool
	Resumed   bool
	UserAgent string
}

type uploadRun struct {
	bytes    int64
	active   time.Duration
	inflight int
	since    time.Time
}

// dayAt returns the key and counters of t's local day, dropping days that fell out of the window
// when t opens a new one; the caller holds s.mu.
func (s *Stats) dayAt(t time.Time) (string, *day) {
	key := t.In(s.loc).Format(dayLayout)
	if key > s.today {
		s.today = key
		s.prune()
	}
	d, ok := s.f.Days[key]
	if !ok {
		d = &day{Visitors: newSketch(globalPrecision)}
		s.f.Days[key] = d
	}
	return key, d
}

// cutoff returns the key of the oldest day still inside the window ending today.
func (s *Stats) cutoff() string {
	t, err := time.ParseInLocation(dayLayout, s.today, s.loc)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, 1-window).Format(dayLayout)
}

func (s *Stats) prune() {
	oldest := s.cutoff()
	for k := range s.f.Days {
		if k < oldest {
			delete(s.f.Days, k)
		}
	}
	for _, sh := range s.f.Shares {
		for k := range sh.Days {
			if k < oldest {
				delete(sh.Days, k)
			}
		}
		for _, fs := range sh.Files {
			for k := range fs.Days {
				if k < oldest {
					delete(fs.Days, k)
				}
			}
		}
	}
}

func (s *Stats) share(name string) *shareStats {
	sh, ok := s.f.Shares[name]
	if !ok {
		sh = &shareStats{Visitors: newSketch(sharePrecision)}
		s.f.Shares[name] = sh
	}
	return sh
}

func (sh *shareStats) day(key string) *shareDay {
	if sh.Days == nil {
		sh.Days = map[string]*shareDay{}
	}
	d, ok := sh.Days[key]
	if !ok {
		d = &shareDay{Visitors: newSketch(sharePrecision)}
		sh.Days[key] = d
	}
	return d
}

func (sh *shareStats) file(path string) *fileStats {
	if sh.Files == nil {
		sh.Files = map[string]*fileStats{}
	}
	fs, ok := sh.Files[path]
	if !ok {
		fs = &fileStats{}
		sh.Files[path] = fs
	}
	return fs
}

// Visit counts client, a shortened address, as a visitor of the share and of the whole server.
func (s *Stats) Visit(share, client, userAgent string) {
	if isBot(userAgent) {
		return
	}
	h := s.hash(client)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	key, d := s.dayAt(now)
	s.f.Visitors.Add(h)
	d.Visitors.Add(h)
	sh := s.share(share)
	sh.Visitors.Add(h)
	sh.day(key).Visitors.Add(h)
	sh.LastAccess = now
	s.changed()
}

// PageView counts the device and browser of one opened share page.
func (s *Stats) PageView(userAgent string) {
	device, browser := classify(userAgent)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, d := s.dayAt(s.now())
	for _, c := range []*counters{&s.f.Total, &d.counters} {
		c.Devices = inc(c.Devices, device)
		if browser != "" {
			c.Browsers = inc(c.Browsers, browser)
		}
	}
	s.changed()
}

func inc(m map[string]int64, k string) map[string]int64 {
	if m == nil {
		m = map[string]int64{}
	}
	m[k]++
	return m
}

// Download marks the start of d and returns the function that records its end with the bytes sent
// and whether the whole response went out. Bots are not counted.
func (s *Stats) Download(d Download) func(sent int64, complete bool) {
	if isBot(d.UserAgent) {
		return func(int64, bool) {}
	}
	s.mu.Lock()
	start := s.now()
	if !d.Resumed {
		s.live++
		_, today := s.dayAt(start)
		for _, p := range []*peak{&s.f.Peak, &today.Peak} {
			if s.live > p.N {
				*p = peak{N: s.live, At: start}
			}
		}
		s.changed()
	}
	s.mu.Unlock()
	return func(sent int64, complete bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.finishDownload(d, start, s.now(), sent, complete)
		s.changed()
	}
}

func (s *Stats) finishDownload(d Download, start, end time.Time, sent int64, complete bool) {
	key, day := s.dayAt(start)
	cs := []*counters{&s.f.Total, &day.counters}
	for _, c := range cs {
		c.BytesDown += sent
	}
	if d.Resumed {
		return
	}
	s.live--
	local := start.In(s.loc)
	s.f.Heatmap[local.Weekday()][local.Hour()]++
	day.Hours[local.Hour()]++
	for _, c := range cs {
		c.Downloads++
		if d.Zip {
			c.ZipDownloads++
		}
		if !complete {
			c.DownloadsIncomplete++
		}
		if complete {
			c.SpeedDown.add(sent, end.Sub(start))
		}
	}
	sh := s.share(d.Share)
	sh.Downloads++
	sh.day(key).Downloads++
	if !d.Zip {
		fs := sh.file(d.File)
		fs.Downloads++
		if fs.Days == nil {
			fs.Days = map[string]int64{}
		}
		fs.Days[key]++
	}
}

func (sp *speed) add(bytes int64, d time.Duration) {
	if bytes < minSpeedBytes || d < minSpeedDuration {
		return
	}
	rate := float64(bytes) / d.Seconds()
	if sp.Count == 0 || rate < sp.Min {
		sp.Min = rate
	}
	if rate > sp.Max {
		sp.Max = rate
	}
	sp.Count++
	sp.Bytes += bytes
	sp.Nanos += d.Nanoseconds()
}

func (s *Stats) run(key string) *uploadRun {
	r, ok := s.uploads[key]
	if !ok {
		r = &uploadRun{}
		s.uploads[key] = r
	}
	return r
}

// UploadStarted begins a fresh measurement for the new upload session named key.
func (s *Stats) UploadStarted(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads[key] = &uploadRun{}
}

// ChunkStarted marks that a chunk of the upload named key is being received.
func (s *Stats) ChunkStarted(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.run(key)
	if r.inflight == 0 {
		r.since = s.now()
	}
	r.inflight++
}

// ChunkFinished records n bytes received by a chunk of the upload named key.
func (s *Stats) ChunkFinished(key string, n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if r, ok := s.uploads[key]; ok && r.inflight > 0 {
		r.inflight--
		if r.inflight == 0 {
			r.active += now.Sub(r.since)
		}
		r.bytes += n
	}
	_, d := s.dayAt(now)
	s.f.Total.BytesUp += n
	d.BytesUp += n
	s.changed()
}

// UploadPublished counts the upload named key as finished.
func (s *Stats) UploadPublished(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.uploads[key]
	delete(s.uploads, key)
	_, d := s.dayAt(s.now())
	for _, c := range []*counters{&s.f.Total, &d.counters} {
		c.Uploads++
		if r != nil {
			c.SpeedUp.add(r.bytes, r.active)
		}
	}
	s.changed()
}

// UploadAbandoned counts the upload named key as removed without being published.
func (s *Stats) UploadAbandoned(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.uploads, key)
	_, d := s.dayAt(s.now())
	s.f.Total.UploadsAbandoned++
	d.UploadsAbandoned++
	s.changed()
}
