package store

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Wirezat/fileshare/internal/config"
)

func benchConfig(b *testing.B, extra string) string {
	b.Helper()
	cfg := config.Config{Files: map[string]config.FileData{}}
	for i := range 100 {
		cfg.Files[fmt.Sprintf("share%03d", i)] = config.FileData{Path: fmt.Sprintf("/srv/share%03d", i)}
	}
	cfg.Files[extra] = config.FileData{Path: "/srv/" + extra}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}

func BenchmarkCurrentNoCheckDue(b *testing.B) {
	s, _, _, _ := clockedStore(b, benchConfig(b, "x"))
	for b.Loop() {
		s.Current()
	}
}

func BenchmarkCurrentCheckDueFileUnchanged(b *testing.B) {
	s, _, c, _ := clockedStore(b, benchConfig(b, "x"))
	for b.Loop() {
		c.advance(checkInterval)
		s.Current()
	}
}

func BenchmarkCurrentCheckDueFileChanged(b *testing.B) {
	s, path, c, _ := clockedStore(b, benchConfig(b, "x"))
	raw := [2]string{benchConfig(b, "a"), benchConfig(b, "bb")}
	var reported int
	s.OnOutsideChange(func(old, next config.View) {
		if _, ok := next.Share("share050"); ok {
			reported++
		}
	})
	i := 0
	for b.Loop() {
		b.StopTimer()
		handEdit(b, path, raw[i%2])
		i++
		c.advance(checkInterval)
		b.StartTimer()
		s.Current()
	}
	if reported != i {
		b.Fatalf("%d changes reported, want %d", reported, i)
	}
}
