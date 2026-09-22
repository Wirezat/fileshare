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
	GoLog.Infof("expiration watcher started (interval: %s)", interval)
	runEvery(interval, func() {
		config, err := shared.LoadConfig()
		if err != nil {
			GoLog.Errorf("failed to load config: %v", err)
			return
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
	})
}

// clientIP resolves the caller's address. Forwarding headers (X-Forwarded-For
// first entry, then CF-Connecting-IP) are honoured only when the immediate peer
// is a loopback or private address.
func clientIP(r *http.Request) string {
	peer := peerIP(r)
	if !isTrustedProxy(peer) {
		return peer
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if cf := r.Header.Get("Cf-Connecting-Ip"); cf != "" {
		return strings.TrimSpace(cf)
	}
	return peer
}

// peerIP is the address of the immediate connection peer.
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isTrustedProxy(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// requestIsHTTPS reports whether the request reached the server over TLS,
// directly or via a trusted proxy's forwarded header (same peer rule as clientIP).
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !isTrustedProxy(peerIP(r)) {
		return false
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.EqualFold(proto, "https")
	}
	return false
}
