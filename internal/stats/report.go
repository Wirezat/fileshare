package stats

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"time"
)

const topN = 10

// Report is everything the stats page shows: all counts since the start and for the last 30 days.
type Report struct {
	Since  time.Time `json:"since"`
	Live   int       `json:"live"`
	Total  Period    `json:"total"`
	Recent Period    `json:"recent"`
	Days   []DayRow  `json:"days"`
}

// Period holds the counts of one time span; Heatmap rows run Monday to Sunday, columns are hours.
type Period struct {
	Visitors            int64            `json:"visitors"`
	Downloads           int64            `json:"downloads"`
	ZipDownloads        int64            `json:"zipDownloads"`
	DownloadsIncomplete int64            `json:"downloadsIncomplete"`
	Uploads             int64            `json:"uploads"`
	UploadsAbandoned    int64            `json:"uploadsAbandoned"`
	BytesDown           int64            `json:"bytesDown"`
	BytesUp             int64            `json:"bytesUp"`
	SpeedDown           Speed            `json:"speedDown"`
	SpeedUp             Speed            `json:"speedUp"`
	Peak                Peak             `json:"peak"`
	Heatmap             [7][24]int64     `json:"heatmap"`
	TopShares           []ShareRow       `json:"topShares"`
	TopFiles            []FileRow        `json:"topFiles"`
	Devices             map[string]int64 `json:"devices"`
	Browsers            map[string]int64 `json:"browsers"`
}

// Speed is in bytes per second; Avg is total bytes over total time of the Count measured transfers.
type Speed struct {
	Count int64   `json:"count"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Avg   float64 `json:"avg"`
}

type Peak struct {
	N  int        `json:"n"`
	At *time.Time `json:"at"`
}

type DayRow struct {
	Date      string `json:"date"`
	Downloads int64  `json:"downloads"`
	Uploads   int64  `json:"uploads"`
	Visitors  int64  `json:"visitors"`
}

type ShareRow struct {
	Name       string    `json:"name"`
	Downloads  int64     `json:"downloads"`
	Visitors   int64     `json:"visitors"`
	LastAccess time.Time `json:"lastAccess"`
	Deleted    bool      `json:"deleted"`
}

type FileRow struct {
	Share     string `json:"share"`
	Path      string `json:"path"`
	Downloads int64  `json:"downloads"`
	Deleted   bool   `json:"deleted"`
}

// Report returns the current counts; exists tells which share names still name a share.
func (s *Stats) Report(exists func(share string) bool) Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := max(s.now().In(s.loc).Format(dayLayout), s.today)
	keys := windowKeys(today, s.loc)

	r := Report{Since: s.f.Since, Live: s.live}
	r.Total = periodFrom(s.f.Total, s.f.Visitors.Estimate(), s.f.Peak)
	r.Total.Heatmap = mondayFirst(s.f.Heatmap)

	var recent counters
	var recentPeak peak
	var hours [7][24]int64
	visitors := newSketch(globalPrecision)
	for _, k := range keys {
		row := DayRow{Date: k}
		if d, ok := s.f.Days[k]; ok {
			recent.add(d.counters)
			visitors.Merge(d.Visitors.Sketch)
			if d.Peak.N > recentPeak.N {
				recentPeak = d.Peak
			}
			wd := weekday(k, s.loc)
			for h, n := range d.Hours {
				hours[wd][h] += n
			}
			row.Downloads, row.Uploads, row.Visitors = d.Downloads, d.Uploads, round(d.Visitors.Estimate())
		}
		r.Days = append(r.Days, row)
	}
	r.Recent = periodFrom(recent, visitors.Estimate(), recentPeak)
	r.Recent.Heatmap = mondayFirst(hours)

	r.Total.TopShares, r.Recent.TopShares = s.topShares(keys, exists)
	r.Total.TopFiles, r.Recent.TopFiles = s.topFiles(keys, exists)
	return r
}

// windowKeys returns the day keys of the window ending on today, oldest first.
func windowKeys(today string, loc *time.Location) []string {
	t, _ := time.ParseInLocation(dayLayout, today, loc)
	keys := make([]string, window)
	for i := range keys {
		keys[i] = t.AddDate(0, 0, i+1-window).Format(dayLayout)
	}
	return keys
}

func weekday(key string, loc *time.Location) time.Weekday {
	t, _ := time.ParseInLocation(dayLayout, key, loc)
	return t.Weekday()
}

func mondayFirst(h [7][24]int64) [7][24]int64 {
	var out [7][24]int64
	for wd := range h {
		out[(wd+6)%7] = h[wd]
	}
	return out
}

func round(f float64) int64 { return int64(math.Round(f)) }

func (c *counters) add(o counters) {
	c.Downloads += o.Downloads
	c.ZipDownloads += o.ZipDownloads
	c.DownloadsIncomplete += o.DownloadsIncomplete
	c.Uploads += o.Uploads
	c.UploadsAbandoned += o.UploadsAbandoned
	c.BytesDown += o.BytesDown
	c.BytesUp += o.BytesUp
	c.SpeedDown.merge(o.SpeedDown)
	c.SpeedUp.merge(o.SpeedUp)
	for k, v := range o.Devices {
		c.Devices = addTo(c.Devices, k, v)
	}
	for k, v := range o.Browsers {
		c.Browsers = addTo(c.Browsers, k, v)
	}
}

func addTo(m map[string]int64, k string, v int64) map[string]int64 {
	if m == nil {
		m = map[string]int64{}
	}
	m[k] += v
	return m
}

func (sp *speed) merge(o speed) {
	if o.Count == 0 {
		return
	}
	if sp.Count == 0 || o.Min < sp.Min {
		sp.Min = o.Min
	}
	sp.Max = max(sp.Max, o.Max)
	sp.Count += o.Count
	sp.Bytes += o.Bytes
	sp.Nanos += o.Nanos
}

func (sp speed) report() Speed {
	out := Speed{Count: sp.Count, Min: sp.Min, Max: sp.Max}
	if sp.Nanos > 0 {
		out.Avg = float64(sp.Bytes) / (float64(sp.Nanos) / 1e9)
	}
	return out
}

func periodFrom(c counters, visitors float64, p peak) Period {
	out := Period{
		Visitors:            round(visitors),
		Downloads:           c.Downloads,
		ZipDownloads:        c.ZipDownloads,
		DownloadsIncomplete: c.DownloadsIncomplete,
		Uploads:             c.Uploads,
		UploadsAbandoned:    c.UploadsAbandoned,
		BytesDown:           c.BytesDown,
		BytesUp:             c.BytesUp,
		SpeedDown:           c.SpeedDown.report(),
		SpeedUp:             c.SpeedUp.report(),
		Peak:                Peak{N: p.N},
		Devices:             maps.Clone(c.Devices),
		Browsers:            maps.Clone(c.Browsers),
		TopShares:           []ShareRow{},
		TopFiles:            []FileRow{},
	}
	if p.N > 0 {
		at := p.At
		out.Peak.At = &at
	}
	if out.Devices == nil {
		out.Devices = map[string]int64{}
	}
	if out.Browsers == nil {
		out.Browsers = map[string]int64{}
	}
	return out
}

func (s *Stats) topShares(keys []string, exists func(string) bool) (total, recent []ShareRow) {
	total, recent = []ShareRow{}, []ShareRow{}
	for name, sh := range s.f.Shares {
		deleted := !exists(name)
		if sh.Downloads > 0 || !sh.Visitors.Empty() {
			total = append(total, ShareRow{name, sh.Downloads, round(sh.Visitors.Estimate()), sh.LastAccess, deleted})
		}
		visitors := newSketch(sharePrecision)
		var downloads int64
		active := false
		for _, k := range keys {
			if d, ok := sh.Days[k]; ok {
				active = true
				downloads += d.Downloads
				visitors.Merge(d.Visitors.Sketch)
			}
		}
		if active {
			recent = append(recent, ShareRow{name, downloads, round(visitors.Estimate()), sh.LastAccess, deleted})
		}
	}
	return topShareRows(total), topShareRows(recent)
}

func topShareRows(rows []ShareRow) []ShareRow {
	slices.SortFunc(rows, func(a, b ShareRow) int {
		return cmp.Or(cmp.Compare(b.Downloads, a.Downloads), cmp.Compare(b.Visitors, a.Visitors), cmp.Compare(a.Name, b.Name))
	})
	return rows[:min(len(rows), topN)]
}

func (s *Stats) topFiles(keys []string, exists func(string) bool) (total, recent []FileRow) {
	total, recent = []FileRow{}, []FileRow{}
	for name, sh := range s.f.Shares {
		deleted := !exists(name)
		for path, fs := range sh.Files {
			if fs.Downloads > 0 {
				total = append(total, FileRow{name, path, fs.Downloads, deleted})
			}
			var n int64
			for _, k := range keys {
				n += fs.Days[k]
			}
			if n > 0 {
				recent = append(recent, FileRow{name, path, n, deleted})
			}
		}
	}
	return topFileRows(total), topFileRows(recent)
}

func topFileRows(rows []FileRow) []FileRow {
	slices.SortFunc(rows, func(a, b FileRow) int {
		return cmp.Or(cmp.Compare(b.Downloads, a.Downloads), cmp.Compare(a.Share, b.Share), cmp.Compare(a.Path, b.Path))
	})
	return rows[:min(len(rows), topN)]
}
