package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wirezat/fileshare/internal/ratelimit"
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

func TestRateKey(t *testing.T) {
	cases := []struct{ peer, want string }{
		{"203.0.113.9:4000", "203.0.113.9"},
		{"[2001:db8:1:2:aaaa::1]:4000", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:bbbb::9]:4000", "2001:db8:1:2::/64"},
		{"[2001:db8:1:3::1]:4000", "2001:db8:1:3::/64"},
		{"[::ffff:203.0.113.9]:4000", "203.0.113.9"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.peer
		if got := rateKey(r); got != c.want {
			t.Errorf("rateKey(%s) = %q, want %q", c.peer, got, c.want)
		}
	}
}

func TestLoginLimitCoversTheWholeIPv6Network(t *testing.T) {
	e := newEnv(t, configWithAdmin(t, "secret"))
	from := func(peer string) func(*http.Request) {
		return func(r *http.Request) { r.RemoteAddr = peer }
	}
	try := func(peer, pass string) int {
		body := `{"username":"admin","password":"` + pass + `"}`
		return send(e.handler, http.MethodPost, "/admin/login", strings.NewReader(body),
			withHeader("Content-Type", "application/json"), from(peer)).Code
	}
	for i := 0; i < ratelimit.MaxFailures; i++ {
		try("[2001:db8:1:2::"+string(rune('a'+i))+"]:4000", "wrong")
	}
	if got := try("[2001:db8:1:2::ff]:4000", "secret"); got != http.StatusTooManyRequests {
		t.Errorf("same /64: status = %d, want 429", got)
	}
	if got := try("[2001:db8:1:3::1]:4000", "secret"); got != http.StatusNoContent {
		t.Errorf("other /64: status = %d, want 204", got)
	}
}
