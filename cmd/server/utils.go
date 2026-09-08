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
// clientIP resolves the caller's address, honouring forwarding headers only
// when the immediate peer is a loopback or private address — i.e. a reverse
// proxy on the same host or LAN.
//
// The headers are trivially forged by whoever connects. Trusting them from any
// peer would let an attacker present a fresh address on every request and walk
// straight through the login rate limiter, which is keyed on this value.
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

// peerIP is the address of whoever opened the connection — never forgeable.
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
// either directly or through a reverse proxy that said so. The forwarded
// header is only believed from a trusted peer, same rule as clientIP.
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
