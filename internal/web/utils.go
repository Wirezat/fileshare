package web

import (
	"net"
	"net/http"
	"strings"
)

// clientIP returns the peer, or behind a loopback/private peer the rightmost public X-Forwarded-For entry.
func clientIP(r *http.Request) string {
	peer := peerIP(r)
	if !isTrustedProxy(peer) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		ip := hopIP(hops[i])
		if ip == "" {
			break
		}
		client = ip
		if !isTrustedProxy(ip) {
			break
		}
	}
	return client
}

func hopIP(hop string) string {
	hop = strings.TrimSpace(hop)
	if host, _, err := net.SplitHostPort(hop); err == nil {
		hop = host
	}
	if ip := net.ParseIP(hop); ip != nil {
		return ip.String()
	}
	return ""
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

// requestIsHTTPS reports whether the request came over TLS, directly or via a trusted proxy.
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
