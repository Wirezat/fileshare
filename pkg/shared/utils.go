package shared

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// IsExpired reports whether a share has expired due to manual expiry,
// exhausted uses, or a passed expiration timestamp.
func IsExpired(fd FileData) bool {
	return fd.Expired || fd.Uses == 0 || (fd.Expiration != 0 && fd.Expiration < time.Now().Unix())
}

// ParseExpiration parses a human-readable expiration string into a Unix timestamp.
// Accepts: "" / "0" / "never" → 0, a plain unix timestamp, or a duration
// suffix: 24h, 7d, 2w, 3m, 1y.
func ParseExpiration(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "never" {
		return 0, nil
	}
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ts, nil
	}
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid expiration %q — use e.g. 24h, 7d, 2w, 3m, 1y or a unix timestamp", s)
	}
	unit := s[len(s)-1]
	num, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, fmt.Errorf("invalid expiration %q: %w", s, err)
	}
	now := time.Now()
	switch unit {
	case 'h':
		return now.Add(time.Duration(num) * time.Hour).Unix(), nil
	case 'd':
		return now.AddDate(0, 0, num).Unix(), nil
	case 'w':
		return now.AddDate(0, 0, num*7).Unix(), nil
	case 'm':
		return now.AddDate(0, num, 0).Unix(), nil
	case 'y':
		return now.AddDate(num, 0, 0).Unix(), nil
	default:
		return 0, fmt.Errorf("unknown unit %q — use h, d, w, m or y", string(unit))
	}
}

// GenerateRandomSubpath generates a random URL-safe subpath of the given length.
func GenerateRandomSubpath(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
