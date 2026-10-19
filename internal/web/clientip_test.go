package web

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name string
		peer string
		xff  []string
		want string
	}{
		{"public peer ignores the header", "203.0.113.9:4000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"proxy without header", "127.0.0.1:4000", nil, "127.0.0.1"},
		{"one proxy", "127.0.0.1:4000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"spoofed entry before the appended one", "127.0.0.1:4000", []string{"1.2.3.4, 198.51.100.7"}, "198.51.100.7"},
		{"proxy chain", "127.0.0.1:4000", []string{"198.51.100.7, 10.0.0.2, 172.16.0.3"}, "198.51.100.7"},
		{"home network client", "127.0.0.1:4000", []string{"192.168.0.23"}, "192.168.0.23"},
		{"all private takes the leftmost", "::1", []string{"192.168.0.23, 10.0.0.2"}, "192.168.0.23"},
		{"repeated headers", "10.0.0.1:4000", []string{"1.2.3.4", "198.51.100.7"}, "198.51.100.7"},
		{"entry with port", "127.0.0.1:4000", []string{"198.51.100.7:5555"}, "198.51.100.7"},
		{"ipv6 entry with port", "127.0.0.1:4000", []string{"[2001:db8::1]:5555"}, "2001:db8::1"},
		{"garbage stops the walk", "127.0.0.1:4000", []string{"198.51.100.7, nonsense, 10.0.0.2"}, "10.0.0.2"},
		{"garbage only", "127.0.0.1:4000", []string{"nonsense"}, "127.0.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.peer
			for _, v := range c.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r); got != c.want {
				t.Errorf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestClientIPIgnoresCfConnectingIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:4000"
	r.Header.Set("Cf-Connecting-Ip", "1.2.3.4")
	if got := clientIP(r); got != "127.0.0.1" {
		t.Errorf("clientIP = %q, want the peer", got)
	}
}
