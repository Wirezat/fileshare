package stats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

var start = time.Date(2026, 10, 19, 14, 30, 0, 0, time.UTC)

func openTest(t *testing.T) (*Stats, *clock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stats.json")
	c := &clock{t: start}
	s := open(path, c.Now, time.UTC)
	s.delay = time.Hour
	s.f.Key = bytes.Repeat([]byte{7}, keySize)
	t.Cleanup(func() { s.Close() })
	return s, c, path
}

func all(string) bool { return true }

func TestOpenMissingFileStartsNow(t *testing.T) {
	s, _, path := openTest(t)
	r := s.Report(all)
	if !r.Since.Equal(start) {
		t.Errorf("Since = %v, want %v", r.Since, start)
	}
	if len(s.f.Key) != keySize {
		t.Errorf("key has %d bytes", len(s.f.Key))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("opening wrote %s before anything was counted", path)
	}
	if len(r.Days) != window || r.Days[window-1].Date != "2026-10-19" || r.Days[0].Date != "2026-09-20" {
		t.Errorf("days run %v .. %v", r.Days[0].Date, r.Days[len(r.Days)-1].Date)
	}
}

func TestCloseWritesAndReopenKeepsCounts(t *testing.T) {
	s, c, path := openTest(t)
	s.Visit("a", "203.0.113.1", browserUA)
	s.Download(Download{Share: "a", File: "/x.zip", UserAgent: browserUA})(10, true)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("stats.json mode %v, want 0600", fi.Mode().Perm())
	}
	again := open(path, c.Now, time.UTC)
	if string(again.f.Key) != string(s.f.Key) {
		t.Error("key changed across reopen")
	}
	again.Visit("a", "203.0.113.1", browserUA)
	r := again.Report(all)
	if r.Total.Visitors != 1 || r.Total.Downloads != 1 || r.Total.BytesDown != 10 || !r.Since.Equal(start) {
		t.Errorf("after reopen: visitors %d downloads %d bytes %d since %v", r.Total.Visitors, r.Total.Downloads, r.Total.BytesDown, r.Since)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only stats.json", len(entries))
	}
}

func TestUnreadableFileIsMovedAside(t *testing.T) {
	good := func(t *testing.T) []byte {
		s, _, _ := openTest(t)
		s.Visit("a", "1.2.3.4", browserUA)
		data, _ := json.Marshal(s.f)
		return data
	}
	for name, mangle := range map[string]func([]byte) []byte{
		"not json":      func([]byte) []byte { return []byte("{nope") },
		"empty":         func([]byte) []byte { return nil },
		"newer version": func(b []byte) []byte { return []byte(strings.Replace(string(b), `"version":1`, `"version":2`, 1)) },
		"short key":     func(b []byte) []byte { return []byte(strings.Replace(string(b), `"key":"`, `"key":"AAAA`, 1)) },
		"bad sketch":    func(b []byte) []byte { return []byte(strings.Replace(string(b), `"visitors":"`, `"visitors":"AQ`, 1)) },
		"bad day":       func(b []byte) []byte { return []byte(strings.Replace(string(b), `"2026-10-19"`, `"yesterday"`, 1)) },
		"null share": func(b []byte) []byte {
			return []byte(strings.Replace(string(b), `"shares":{`, `"shares":{"z":null,`, 1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "stats.json")
			bad := mangle(good(t))
			os.WriteFile(path, bad, 0o600)
			c := &clock{t: start.Add(time.Hour)}
			s := open(path, c.Now, time.UTC)
			if r := s.Report(all); r.Total.Visitors != 0 || !r.Since.Equal(c.Now()) {
				t.Errorf("did not start over: visitors %d since %v", r.Total.Visitors, r.Since)
			}
			aside := path + ".broken-20261019-153000"
			kept, err := os.ReadFile(aside)
			if err != nil || string(kept) != string(bad) {
				t.Errorf("broken file not kept at %s: %v", aside, err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Error("broken file still in place")
			}
		})
	}
}

func TestVisitCountsDistinctClientsAndSkipsBots(t *testing.T) {
	s, _, _ := openTest(t)
	for i := range 50 {
		s.Visit("a", fmt.Sprintf("203.0.113.%d", i), browserUA)
		s.Visit("a", fmt.Sprintf("203.0.113.%d", i), browserUA)
	}
	s.Visit("b", "203.0.113.1", browserUA)
	s.Visit("b", "198.51.100.7", "WhatsApp/2.23")
	s.Visit("b", "198.51.100.8", "")
	s.Visit("b", "198.51.100.9", "curl/8.0")
	r := s.Report(all)
	near := func(got, want, tol int64) bool { return got >= want-tol && got <= want+tol }
	if !near(r.Total.Visitors, 50, 2) || r.Recent.Visitors != r.Total.Visitors {
		t.Errorf("visitors total %d recent %d, want about 50", r.Total.Visitors, r.Recent.Visitors)
	}
	rows := map[string]ShareRow{}
	for _, row := range r.Total.TopShares {
		rows[row.Name] = row
	}
	if !near(rows["a"].Visitors, 50, 5) || rows["b"].Visitors != 1 {
		t.Errorf("share visitors a=%d b=%d, want about 50 and 1", rows["a"].Visitors, rows["b"].Visitors)
	}
	if !rows["b"].LastAccess.Equal(start) {
		t.Errorf("last access of b = %v", rows["b"].LastAccess)
	}
	if r.Days[window-1].Visitors != r.Total.Visitors {
		t.Errorf("today's visitors %d", r.Days[window-1].Visitors)
	}
}

func TestVisitorHashIsKeyed(t *testing.T) {
	a, _, _ := openTest(t)
	b, _, _ := openTest(t)
	b.f.Key = bytes.Repeat([]byte{8}, keySize)
	if a.hash("203.0.113.1") == b.hash("203.0.113.1") {
		t.Error("two stats files hash an address the same way")
	}
	if a.hash("203.0.113.1") != a.hash("203.0.113.1") {
		t.Error("hash is not stable")
	}
	x := open(filepath.Join(t.TempDir(), "stats.json"), time.Now, time.UTC)
	y := open(filepath.Join(t.TempDir(), "stats.json"), time.Now, time.UTC)
	if bytes.Equal(x.f.Key, y.f.Key) {
		t.Error("two new stats files got the same key")
	}
}

func TestStatsFileHoldsNoAddressOrAgent(t *testing.T) {
	s, _, path := openTest(t)
	s.Visit("a", "203.0.113.77", browserUA)
	s.PageView(browserUA)
	s.Download(Download{Share: "a", File: "/f", UserAgent: browserUA})(1, true)
	s.Close()
	data, _ := os.ReadFile(path)
	for _, leak := range []string{"203.0.113.77", "Mozilla", "Chrome/130"} {
		if strings.Contains(string(data), leak) {
			t.Errorf("stats.json contains %q", leak)
		}
	}
}

func TestDownloadCounting(t *testing.T) {
	s, c, _ := openTest(t)
	file := Download{Share: "a", File: "/big.iso", UserAgent: browserUA}
	end := s.Download(file)
	c.Advance(2 * time.Second)
	end(4<<20, true)
	s.Download(file)(1000, false)
	s.Download(Download{Share: "a", File: "/big.iso", Resumed: true, UserAgent: browserUA})(500, true)
	s.Download(Download{Share: "a", Zip: true, UserAgent: browserUA})(300, true)
	s.Download(Download{Share: "a", File: "/big.iso", UserAgent: "Discordbot/2.0"})(999, true)

	p := s.Report(all).Total
	if p.Downloads != 3 || p.ZipDownloads != 1 || p.DownloadsIncomplete != 1 {
		t.Errorf("downloads %d zip %d incomplete %d, want 3 1 1", p.Downloads, p.ZipDownloads, p.DownloadsIncomplete)
	}
	if want := int64(4<<20 + 1000 + 500 + 300); p.BytesDown != want {
		t.Errorf("bytes down %d, want %d", p.BytesDown, want)
	}
	if len(p.TopFiles) != 1 || p.TopFiles[0].Downloads != 2 || p.TopFiles[0].Path != "/big.iso" {
		t.Errorf("top files %+v, want /big.iso with 2", p.TopFiles)
	}
	if len(p.TopShares) != 1 || p.TopShares[0].Downloads != 3 {
		t.Errorf("top shares %+v, want a with 3", p.TopShares)
	}
	if p.Heatmap[0][14] != 3 {
		t.Errorf("Monday 14:00 has %d downloads, want 3", p.Heatmap[0][14])
	}
	if p.SpeedDown.Count != 1 || p.SpeedDown.Avg != 2<<20 {
		t.Errorf("speed %+v, want one transfer at 2 MiB/s", p.SpeedDown)
	}
}

func TestHeatmapUsesStartAndLocalTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip(err)
	}
	c := &clock{t: time.Date(2026, 10, 25, 22, 59, 0, 0, time.UTC)}
	s := open(path, c.Now, berlin)
	end := s.Download(Download{Share: "a", File: "/f", UserAgent: browserUA})
	c.Advance(2 * time.Minute)
	end(1, true)
	r := s.Report(all)
	if r.Total.Heatmap[6][23] != 1 {
		t.Errorf("Sunday 23:00 Berlin has %d, want 1", r.Total.Heatmap[6][23])
	}
	if r.Recent.Heatmap[6][23] != 1 || r.Days[window-2].Downloads != 1 {
		t.Errorf("recent heatmap %d, day before today %+v", r.Recent.Heatmap[6][23], r.Days[window-2])
	}
}

func TestSpeedMeasuresOnlyLongCompleteTransfers(t *testing.T) {
	s, c, _ := openTest(t)
	run := func(bytes int64, d time.Duration, complete bool) {
		end := s.Download(Download{Share: "a", File: "/f", UserAgent: browserUA})
		c.Advance(d)
		end(bytes, complete)
	}
	run(10<<20, 10*time.Second, true)
	run(2<<20, time.Second, true)
	run(1<<20-1, 10*time.Second, true)
	run(5<<20, 999*time.Millisecond, true)
	run(50<<20, 5*time.Second, false)
	sp := s.Report(all).Total.SpeedDown
	if sp.Count != 2 || sp.Min != 1<<20 || sp.Max != 2<<20 {
		t.Errorf("speed %+v, want 2 transfers between 1 and 2 MiB/s", sp)
	}
	if want := float64(12<<20) / 11; math.Abs(sp.Avg-want) > 1e-6 {
		t.Errorf("avg %.1f, want byte-weighted %.1f", sp.Avg, want)
	}
}

func TestLiveAndPeak(t *testing.T) {
	s, c, _ := openTest(t)
	a := s.Download(Download{Share: "a", File: "/1", UserAgent: browserUA})
	c.Advance(time.Minute)
	b := s.Download(Download{Share: "a", File: "/2", UserAgent: browserUA})
	resumed := s.Download(Download{Share: "a", File: "/2", Resumed: true, UserAgent: browserUA})
	if live := s.Report(all).Live; live != 2 {
		t.Errorf("live %d, want 2", live)
	}
	a(1, true)
	b(1, true)
	resumed(1, true)
	c.Advance(time.Minute)
	s.Download(Download{Share: "a", File: "/3", UserAgent: browserUA})(1, true)
	r := s.Report(all)
	if r.Live != 0 {
		t.Errorf("live %d after all ended", r.Live)
	}
	want := start.Add(time.Minute)
	for name, p := range map[string]Peak{"total": r.Total.Peak, "recent": r.Recent.Peak} {
		if p.N != 2 || p.At == nil || !p.At.Equal(want) {
			t.Errorf("%s peak %+v, want 2 at %v", name, p, want)
		}
	}
}

func TestUploadCounting(t *testing.T) {
	s, c, _ := openTest(t)
	s.ChunkStarted("u1")
	c.Advance(time.Second)
	s.ChunkStarted("u1")
	c.Advance(time.Second)
	s.ChunkFinished("u1", 1<<20)
	c.Advance(time.Second)
	s.ChunkFinished("u1", 1<<20)
	c.Advance(time.Hour)
	s.ChunkStarted("u1")
	c.Advance(time.Second)
	s.ChunkFinished("u1", 1<<20)
	s.UploadPublished("u1")

	s.ChunkStarted("u2")
	s.ChunkFinished("u2", 700)
	s.UploadAbandoned("u2")
	s.UploadAbandoned("from-before-restart")
	s.UploadPublished("unknown")

	p := s.Report(all).Total
	if p.Uploads != 2 || p.UploadsAbandoned != 2 {
		t.Errorf("uploads %d abandoned %d, want 2 and 2", p.Uploads, p.UploadsAbandoned)
	}
	if want := int64(3<<20 + 700); p.BytesUp != want {
		t.Errorf("bytes up %d, want %d", p.BytesUp, want)
	}
	if p.SpeedUp.Count != 1 || p.SpeedUp.Avg != float64(3<<20)/4 {
		t.Errorf("upload speed %+v, want 3 MiB over 4 active seconds", p.SpeedUp)
	}
	if len(s.uploads) != 0 {
		t.Errorf("%d upload runs left in memory", len(s.uploads))
	}
}

func TestNewUploadSessionForgetsAStaleRun(t *testing.T) {
	s, c, _ := openTest(t)
	s.ChunkStarted("k")
	c.Advance(time.Hour)
	s.ChunkFinished("k", 50<<20)
	s.UploadStarted("k")
	s.ChunkStarted("k")
	c.Advance(2 * time.Second)
	s.ChunkFinished("k", 4<<20)
	s.UploadPublished("k")
	if sp := s.Report(all).Total.SpeedUp; sp.Count != 1 || sp.Avg != 2<<20 {
		t.Errorf("upload speed %+v, want one upload at 2 MiB/s", sp)
	}
}

func TestWindowDropsOldDays(t *testing.T) {
	s, c, _ := openTest(t)
	s.Visit("old", "203.0.113.1", browserUA)
	s.Download(Download{Share: "old", File: "/f", UserAgent: browserUA})(5, true)
	s.ChunkStarted("u")
	s.ChunkFinished("u", 9)
	s.UploadPublished("u")

	c.Advance(29 * 24 * time.Hour)
	s.Visit("new", "203.0.113.2", browserUA)
	r := s.Report(all)
	if r.Recent.Downloads != 1 || r.Recent.Visitors != 2 || r.Days[0].Downloads != 1 {
		t.Errorf("day 30: recent downloads %d visitors %d, first row %+v", r.Recent.Downloads, r.Recent.Visitors, r.Days[0])
	}

	c.Advance(24 * time.Hour)
	s.Visit("new", "203.0.113.2", browserUA)
	r = s.Report(all)
	if r.Recent.Downloads != 0 || r.Recent.Uploads != 0 || r.Recent.BytesDown != 0 || r.Recent.Visitors != 1 {
		t.Errorf("day 31: recent %+v", r.Recent)
	}
	if r.Total.Downloads != 1 || r.Total.Uploads != 1 || r.Total.Visitors != 2 {
		t.Errorf("day 31: total downloads %d uploads %d visitors %d", r.Total.Downloads, r.Total.Uploads, r.Total.Visitors)
	}
	if len(r.Recent.TopShares) != 1 || r.Recent.TopShares[0].Name != "new" || len(r.Recent.TopFiles) != 0 {
		t.Errorf("day 31: recent tops %+v %+v", r.Recent.TopShares, r.Recent.TopFiles)
	}
	if len(r.Total.TopFiles) != 1 {
		t.Errorf("day 31: total top files %+v", r.Total.TopFiles)
	}
	if _, ok := s.f.Days["2026-10-19"]; ok {
		t.Error("day 2026-10-19 still stored")
	}
	if len(s.f.Shares["old"].Days) != 0 || len(s.f.Shares["old"].Files["/f"].Days) != 0 {
		t.Error("per-share or per-file days outside the window still stored")
	}
}

func TestClockGoingBackKeepsWindow(t *testing.T) {
	s, c, _ := openTest(t)
	s.Visit("a", "203.0.113.1", browserUA)
	c.Advance(-3 * 24 * time.Hour)
	s.Download(Download{Share: "a", File: "/f", UserAgent: browserUA})(1, true)
	r := s.Report(all)
	if r.Days[window-1].Date != "2026-10-19" || r.Days[window-4].Downloads != 1 || r.Recent.Visitors != 1 {
		t.Errorf("after the clock went back: last row %s, row -3 %+v, visitors %d", r.Days[window-1].Date, r.Days[window-4], r.Recent.Visitors)
	}
}

func TestDeletedSharesKeepTheirNumbers(t *testing.T) {
	s, _, _ := openTest(t)
	s.Download(Download{Share: "gone", File: "/f", UserAgent: browserUA})(1, true)
	s.Download(Download{Share: "here", File: "/f", UserAgent: browserUA})(1, true)
	r := s.Report(func(name string) bool { return name == "here" })
	for _, row := range r.Total.TopShares {
		if row.Deleted != (row.Name == "gone") {
			t.Errorf("share %s deleted=%v", row.Name, row.Deleted)
		}
	}
	for _, row := range r.Recent.TopFiles {
		if row.Deleted != (row.Share == "gone") {
			t.Errorf("file of %s deleted=%v", row.Share, row.Deleted)
		}
	}
}

func TestTopListsAreSortedAndCapped(t *testing.T) {
	s, _, _ := openTest(t)
	for i := range 15 {
		for range i {
			s.Download(Download{Share: fmt.Sprintf("s%02d", i), File: fmt.Sprintf("/f%02d", i), UserAgent: browserUA})(1, true)
		}
	}
	p := s.Report(all).Total
	if len(p.TopShares) != topN || len(p.TopFiles) != topN {
		t.Fatalf("%d shares, %d files, want %d each", len(p.TopShares), len(p.TopFiles), topN)
	}
	if p.TopShares[0].Name != "s14" || p.TopShares[9].Name != "s05" || p.TopFiles[0].Path != "/f14" {
		t.Errorf("order: first %s last %s first file %s", p.TopShares[0].Name, p.TopShares[9].Name, p.TopFiles[0].Path)
	}
}

func TestPageViewCountsDevicesAndBrowsers(t *testing.T) {
	s, _, _ := openTest(t)
	s.PageView(browserUA)
	s.PageView("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1")
	s.PageView("TelegramBot (like TwitterBot)")
	r := s.Report(all)
	for _, p := range []Period{r.Total, r.Recent} {
		if p.Devices[DeviceDesktop] != 1 || p.Devices[DeviceMobile] != 1 || p.Devices[DeviceBot] != 1 {
			t.Errorf("devices %v", p.Devices)
		}
		if p.Browsers[BrowserChrome] != 1 || p.Browsers[BrowserSafari] != 1 || len(p.Browsers) != 2 {
			t.Errorf("browsers %v", p.Browsers)
		}
	}
}

func TestClassify(t *testing.T) {
	for _, c := range []struct{ ua, device, browser string }{
		{browserUA, DeviceDesktop, BrowserChrome},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36 Edg/130.0", DeviceDesktop, BrowserEdge},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", DeviceDesktop, BrowserFirefox},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15", DeviceDesktop, BrowserSafari},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/130.0 Mobile Safari/537.36", DeviceMobile, BrowserChrome},
		{"Mozilla/5.0 (Linux; Android 13; SM-X700) AppleWebKit/537.36 Chrome/130.0 Safari/537.36", DeviceTablet, BrowserChrome},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 CriOS/130.0 Mobile/15E148 Safari/604.1", DeviceTablet, BrowserChrome},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 FxiOS/131.0 Mobile/15E148 Safari/605.1.15", DeviceMobile, BrowserFirefox},
		{"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/130.0 Safari/537.36 OPR/115.0", DeviceDesktop, BrowserOther},
		{"Lynx/2.9", DeviceDesktop, BrowserOther},
		{"", DeviceBot, ""},
		{"Googlebot/2.1", DeviceBot, ""},
		{"Wget/1.21", DeviceBot, ""},
		{"python-requests/2.31", DeviceBot, ""},
		{"Go-http-client/1.1", DeviceBot, ""},
		{"facebookexternalhit/1.1", DeviceBot, ""},
		{"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", DeviceBot, ""},
		{"DuckDuckBot-Https/1.1; (+https://duckduckgo.com/duckduckbot)", DeviceBot, ""},
		{"Mozilla/5.0 (compatible; YandexBot/3.0)", DeviceBot, ""},
		{"Mozilla/5.0 (Linux; Android 10; CUBOT X30) AppleWebKit/537.36 Chrome/130.0 Mobile Safari/537.36", DeviceMobile, BrowserChrome},
	} {
		d, b := classify(c.ua)
		if d != c.device || b != c.browser {
			t.Errorf("classify(%q) = %s, %s; want %s, %s", c.ua, d, b, c.device, c.browser)
		}
	}
}

func TestSaveIsDelayedAndBundled(t *testing.T) {
	s, _, path := openTest(t)
	s.delay = 50 * time.Millisecond
	s.Visit("a", "203.0.113.1", browserUA)
	s.Visit("a", "203.0.113.2", browserUA)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("written before the delay")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil {
			var f file
			if err := json.Unmarshal(data, &f); err != nil {
				t.Fatal(err)
			}
			if got := f.Visitors.Estimate(); math.Round(got) != 2 {
				t.Errorf("saved %v visitors, want 2", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("never written")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConcurrentUse(t *testing.T) {
	s, _, _ := openTest(t)
	s.delay = time.Millisecond
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				s.Visit("a", fmt.Sprintf("10.%d.%d.1", g, i), browserUA)
				s.PageView(browserUA)
				s.Download(Download{Share: "a", File: "/f", UserAgent: browserUA})(1, true)
				key := fmt.Sprintf("u%d-%d", g, i)
				s.ChunkStarted(key)
				s.ChunkFinished(key, 1)
				s.UploadPublished(key)
				if i%50 == 0 {
					s.Report(all)
				}
			}
		})
	}
	wg.Wait()
	r := s.Report(all)
	if r.Total.Downloads != 1600 || r.Total.Uploads != 1600 || r.Live != 0 {
		t.Errorf("downloads %d uploads %d live %d", r.Total.Downloads, r.Total.Uploads, r.Live)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkVisit(b *testing.B) { benchmarkVisit(b, false) }

func BenchmarkVisitWhileSaving(b *testing.B) { benchmarkVisit(b, true) }

func benchmarkVisit(b *testing.B, saving bool) {
	s := open(filepath.Join(b.TempDir(), "stats.json"), time.Now, time.UTC)
	s.delay = time.Hour
	for i := range 200 {
		share := fmt.Sprintf("share-%d", i)
		for j := range 20 {
			s.Visit(share, fmt.Sprintf("10.0.%d.%d", i, j), browserUA)
			s.Download(Download{Share: share, File: fmt.Sprintf("/file-%d", j), UserAgent: browserUA})(1, true)
		}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				if !saving {
					time.Sleep(time.Millisecond)
					continue
				}
				s.save()
				s.Report(all)
			}
		}
	}()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Visit("share-1", fmt.Sprintf("192.0.2.%d", i%250), browserUA)
			i++
		}
	})
	b.StopTimer()
	close(stop)
	<-done
}

func BenchmarkSave(b *testing.B) {
	s := open(filepath.Join(b.TempDir(), "stats.json"), time.Now, time.UTC)
	s.delay = time.Hour
	for i := range 200 {
		share := fmt.Sprintf("share-%d", i)
		for j := range 20 {
			s.Visit(share, fmt.Sprintf("10.0.%d.%d", i, j), browserUA)
			s.Download(Download{Share: share, File: fmt.Sprintf("/file-%d", j), UserAgent: browserUA})(1, true)
		}
	}
	b.Run("locked", func(b *testing.B) {
		for b.Loop() {
			s.mu.Lock()
			s.f.clone()
			s.mu.Unlock()
		}
	})
	b.Run("whole", func(b *testing.B) {
		for b.Loop() {
			s.save()
		}
	})
}

func TestCloneSharesNothing(t *testing.T) {
	s, _, _ := openTest(t)
	s.Visit("a", "203.0.113.1", browserUA)
	s.PageView(browserUA)
	s.Download(Download{Share: "a", File: "/f", UserAgent: browserUA})(1, true)
	before, _ := json.Marshal(s.f)
	c := s.f.clone()
	s.Visit("a", "203.0.113.2", browserUA)
	s.Visit("b", "203.0.113.3", browserUA)
	s.PageView("Mozilla/5.0 (iPhone) Mobile Safari/604.1")
	s.Download(Download{Share: "a", File: "/f", UserAgent: browserUA})(1, true)
	s.Download(Download{Share: "a", File: "/g", UserAgent: browserUA})(1, true)
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Error("changes after clone reached the copy")
	}
}
