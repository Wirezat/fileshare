package stats

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/Wirezat/fileshare/internal/hll"
)

const (
	formatVersion   = 1
	globalPrecision = 14
	sharePrecision  = 8
	keySize         = 32
	dayLayout       = "2006-01-02"
)

// file is the content of stats.json. Counters add up, sketches merge and peaks take the maximum
// when two files are combined.
type file struct {
	Version  int                    `json:"version"`
	Since    time.Time              `json:"since"`
	Key      []byte                 `json:"key"`
	Total    counters               `json:"total"`
	Visitors sketch                 `json:"visitors"`
	Heatmap  [7][24]int64           `json:"heatmap"`
	Peak     peak                   `json:"peak"`
	Days     map[string]*day        `json:"days"`
	Shares   map[string]*shareStats `json:"shares"`
}

type counters struct {
	Downloads           int64            `json:"downloads"`
	ZipDownloads        int64            `json:"zipDownloads"`
	DownloadsIncomplete int64            `json:"downloadsIncomplete"`
	Uploads             int64            `json:"uploads"`
	UploadsAbandoned    int64            `json:"uploadsAbandoned"`
	BytesDown           int64            `json:"bytesDown"`
	BytesUp             int64            `json:"bytesUp"`
	SpeedDown           speed            `json:"speedDown"`
	SpeedUp             speed            `json:"speedUp"`
	Devices             map[string]int64 `json:"devices,omitempty"`
	Browsers            map[string]int64 `json:"browsers,omitempty"`
}

// speed sums the transfers fast and long enough to be measured; Min and Max are in bytes per second.
type speed struct {
	Count int64   `json:"count"`
	Bytes int64   `json:"bytes"`
	Nanos int64   `json:"nanos"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

type peak struct {
	N  int       `json:"n"`
	At time.Time `json:"at"`
}

type day struct {
	counters
	Visitors sketch    `json:"visitors"`
	Hours    [24]int64 `json:"hours"`
	Peak     peak      `json:"peak"`
}

type shareStats struct {
	Downloads  int64                 `json:"downloads"`
	LastAccess time.Time             `json:"lastAccess"`
	Visitors   sketch                `json:"visitors"`
	Days       map[string]*shareDay  `json:"days,omitempty"`
	Files      map[string]*fileStats `json:"files,omitempty"`
}

type shareDay struct {
	Downloads int64  `json:"downloads"`
	Visitors  sketch `json:"visitors"`
}

type fileStats struct {
	Downloads int64            `json:"downloads"`
	Days      map[string]int64 `json:"days,omitempty"`
}

// sketch stores an hll.Sketch in JSON as the base64 of its binary encoding.
type sketch struct {
	*hll.Sketch
}

func newSketch(p int) sketch {
	s, _ := hll.New(p)
	return sketch{s}
}

func (s sketch) MarshalJSON() ([]byte, error) {
	data, err := s.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return json.Marshal(data)
}

func (s *sketch) UnmarshalJSON(b []byte) error {
	var data []byte
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	var h hll.Sketch
	if err := h.UnmarshalBinary(data); err != nil {
		return err
	}
	s.Sketch = &h
	return nil
}

// clone returns a deep copy of f that shares no maps or sketches with it.
func (f *file) clone() *file {
	out := *f
	out.Total = f.Total.clone()
	out.Visitors = f.Visitors.clone()
	out.Days = make(map[string]*day, len(f.Days))
	for k, d := range f.Days {
		c := *d
		c.counters = d.counters.clone()
		c.Visitors = d.Visitors.clone()
		out.Days[k] = &c
	}
	out.Shares = make(map[string]*shareStats, len(f.Shares))
	for name, sh := range f.Shares {
		c := *sh
		c.Visitors = sh.Visitors.clone()
		c.Days = make(map[string]*shareDay, len(sh.Days))
		for k, d := range sh.Days {
			c.Days[k] = &shareDay{Downloads: d.Downloads, Visitors: d.Visitors.clone()}
		}
		c.Files = make(map[string]*fileStats, len(sh.Files))
		for path, fs := range sh.Files {
			c.Files[path] = &fileStats{Downloads: fs.Downloads, Days: maps.Clone(fs.Days)}
		}
		out.Shares[name] = &c
	}
	return &out
}

func (c counters) clone() counters {
	c.Devices = maps.Clone(c.Devices)
	c.Browsers = maps.Clone(c.Browsers)
	return c
}

func (s sketch) clone() sketch { return sketch{s.Clone()} }

func newFile(now time.Time, key []byte) *file {
	return &file{
		Version:  formatVersion,
		Since:    now,
		Key:      key,
		Visitors: newSketch(globalPrecision),
		Days:     map[string]*day{},
		Shares:   map[string]*shareStats{},
	}
}

// check reports the first way f could not have been written by this version.
func (f *file) check() error {
	switch {
	case f.Version != formatVersion:
		return fmt.Errorf("format version %d, want %d", f.Version, formatVersion)
	case len(f.Key) != keySize:
		return fmt.Errorf("key has %d bytes, want %d", len(f.Key), keySize)
	}
	if err := wantPrecision(f.Visitors, globalPrecision); err != nil {
		return err
	}
	if f.Days == nil {
		f.Days = map[string]*day{}
	}
	if f.Shares == nil {
		f.Shares = map[string]*shareStats{}
	}
	for k, d := range f.Days {
		if _, err := time.Parse(dayLayout, k); err != nil || d == nil {
			return fmt.Errorf("day %q is malformed", k)
		}
		if err := wantPrecision(d.Visitors, globalPrecision); err != nil {
			return err
		}
	}
	for name, sh := range f.Shares {
		if sh == nil {
			return fmt.Errorf("share %q is empty", name)
		}
		if err := wantPrecision(sh.Visitors, sharePrecision); err != nil {
			return err
		}
		for k, d := range sh.Days {
			if _, err := time.Parse(dayLayout, k); err != nil || d == nil {
				return fmt.Errorf("day %q of share %q is malformed", k, name)
			}
			if err := wantPrecision(d.Visitors, sharePrecision); err != nil {
				return err
			}
		}
		for path, fs := range sh.Files {
			if fs == nil {
				return fmt.Errorf("file %q of share %q is empty", path, name)
			}
		}
	}
	return nil
}

func wantPrecision(s sketch, p int) error {
	if s.Sketch == nil {
		return fmt.Errorf("missing sketch")
	}
	if s.Precision() != p {
		return fmt.Errorf("sketch precision %d, want %d", s.Precision(), p)
	}
	return nil
}
