package clientip

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestOf(t *testing.T) {
	r := &Resolver{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	cases := []struct {
		remote, xff, want string
	}{
		// Untrusted peer: its headers are ignored.
		{"203.0.113.9:1234", "1.2.3.4", "203.0.113.9"},
		// Trusted proxy: the rightmost untrusted hop is the client, whatever
		// the client prepended.
		{"10.0.0.2:80", "6.6.6.6, 198.51.100.7", "198.51.100.7"},
		{"10.0.0.2:80", "198.51.100.7, 10.0.0.5", "198.51.100.7"},
		// A malformed hop stops the walk: nothing left of it is trusted.
		{"10.0.0.2:80", "198.51.100.7, garbage", "10.0.0.2"},
		{"10.0.0.2:80", "", "10.0.0.2"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = c.remote
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := r.Of(req); got != c.want {
			t.Errorf("%s / %q: got %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
}
