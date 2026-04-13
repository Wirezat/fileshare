package main

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

// startExpirationWatcher polls the config at the given interval and marks
// shares as expired when IsExpired returns true. Runs as a background goroutine.
func startExpirationWatcher(interval time.Duration) {
	go func() {
		GoLog.Infof("expiration watcher started (interval: %s)", interval)
		for {
			time.Sleep(interval)

			config, err := shared.LoadConfig()
			if err != nil {
				GoLog.Errorf("failed to load config: %v", err)
				continue
			}

			changed := false
			for subpath, fd := range config.Files {
				if !fd.Expired && shared.IsExpired(fd) {
					fd.Expired = true
					config.Files[subpath] = fd
					changed = true
					GoLog.Infof("file expired: %s", subpath)
				}
			}

			if changed {
				if err := shared.SaveConfig(config); err != nil {
					GoLog.Errorf("failed to save config after expiration update: %v", err)
				}
			}
		}
	}()
}

// clientIP extracts the real client IP.
// Priority: X-Forwarded-For (first entry) → CF-Connecting-IP → RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if cf := r.Header.Get("Cf-Connecting-Ip"); cf != "" {
		return strings.TrimSpace(cf)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
