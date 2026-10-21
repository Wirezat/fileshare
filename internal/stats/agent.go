package stats

import (
	"strings"

	"github.com/Wirezat/fileshare/internal/media"
)

const (
	DeviceDesktop = "desktop"
	DeviceMobile  = "mobile"
	DeviceTablet  = "tablet"
	DeviceBot     = "bot"

	BrowserChrome  = "chrome"
	BrowserSafari  = "safari"
	BrowserFirefox = "firefox"
	BrowserEdge    = "edge"
	BrowserOther   = "other"
)

var botMarkers = []string{"crawler", "spider", "curl", "wget", "python", "go-http", "java", "+http"}

// isBot reports whether userAgent is empty, a link-preview crawler or a known bot or script; a
// product name ending in "bot" counts when a version, separator or the end follows it.
func isBot(userAgent string) bool {
	if userAgent == "" || media.IsPreviewBot(userAgent) {
		return true
	}
	ua := strings.ToLower(userAgent)
	for _, m := range botMarkers {
		if strings.Contains(ua, m) {
			return true
		}
	}
	for rest := ua; ; {
		i := strings.Index(rest, "bot")
		if i < 0 {
			return false
		}
		rest = rest[i+3:]
		if rest == "" || strings.ContainsRune("/;)-_", rune(rest[0])) {
			return true
		}
	}
}

// classify returns the device class of userAgent and, unless it is a bot, its browser family.
func classify(userAgent string) (device, browser string) {
	if isBot(userAgent) {
		return DeviceBot, ""
	}
	switch {
	case strings.Contains(userAgent, "iPad"), strings.Contains(userAgent, "Tablet"),
		strings.Contains(userAgent, "Android") && !strings.Contains(userAgent, "Mobile"):
		device = DeviceTablet
	case strings.Contains(userAgent, "Mobi"), strings.Contains(userAgent, "iPhone"):
		device = DeviceMobile
	default:
		device = DeviceDesktop
	}
	switch {
	case strings.Contains(userAgent, "Edg"):
		browser = BrowserEdge
	case strings.Contains(userAgent, "OPR/"), strings.Contains(userAgent, "SamsungBrowser"):
		browser = BrowserOther
	case strings.Contains(userAgent, "Firefox/"), strings.Contains(userAgent, "FxiOS"):
		browser = BrowserFirefox
	case strings.Contains(userAgent, "Chrome/"), strings.Contains(userAgent, "CriOS"):
		browser = BrowserChrome
	case strings.Contains(userAgent, "Safari/"):
		browser = BrowserSafari
	default:
		browser = BrowserOther
	}
	return device, browser
}
