// Package clientip works out the address of whoever is on the other end.
//
// X-Forwarded-For is a list anyone can start: a client can send
// "X-Forwarded-For: 1.2.3.4" and a naive reader will believe it. The only
// trustworthy entries are the ones appended by proxies we operate, so the
// list is read from the right, skipping hops inside TrustedProxies, and the
// first address that is not ours is the client. When the direct peer is not
// a trusted proxy, the headers are ignored altogether.
package clientip

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type Resolver struct{ Trusted []netip.Prefix }

func (r *Resolver) trusted(ip netip.Addr) bool {
	for _, p := range r.Trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Of returns the client address of req.
func (r *Resolver) Of(req *http.Request) string {
	peer := peerAddr(req.RemoteAddr)
	if !peer.IsValid() {
		return req.RemoteAddr
	}
	if !r.trusted(peer) {
		return peer.String()
	}
	hops := []string{}
	for _, h := range req.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break // a malformed hop: stop trusting the rest of the list
		}
		ip = ip.Unmap()
		if !r.trusted(ip) {
			return ip.String()
		}
		peer = ip
	}
	// Every hop is ours (a request from inside the network): the leftmost
	// trusted hop is the best answer there is.
	return peer.String()
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap()
}

type ctxKey struct{}

// Middleware stores the resolved address in the request context, replacing
// chi's RealIP, which trusts the headers of anyone.
func (r *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), ctxKey{}, r.Of(req))
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

// From returns the address stored by Middleware, or the raw peer.
func From(req *http.Request) string {
	if v, ok := req.Context().Value(ctxKey{}).(string); ok {
		return v
	}
	if ip := peerAddr(req.RemoteAddr); ip.IsValid() {
		return ip.String()
	}
	return req.RemoteAddr
}
