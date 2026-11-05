package ai

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// httpClient is shared by every provider call.
//
// The "local" and "custom" providers take a base URL from the browser, so the
// server makes requests to an address a user chose. Loopback and private
// networks stay reachable on purpose (Ollama on the same host or LAN is the
// whole point of that provider), but link-local addresses do not: that range
// is where cloud metadata services live (169.254.169.254, fd00:ec2::254), and
// handing them a request on a user's behalf is how instance credentials leak.
// The check sits in the dialer, so it also holds after redirects and DNS
// tricks that resolve a harmless-looking name to a forbidden address.
var httpClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   refuseForbiddenAddr,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 3 * time.Minute,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

var errForbiddenAddr = errors.New("this address is not reachable from the server")

func refuseForbiddenAddr(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() || ip == awsIPv6Metadata {
		return errForbiddenAddr
	}
	return nil
}

var awsIPv6Metadata = netip.MustParseAddr("fd00:ec2::254")

// NormalizeBaseURL validates a user-supplied provider URL and returns it
// without a trailing slash. Only plain http(s) URLs with a host are accepted:
// no credentials in the URL, no query or fragment that would swallow the
// "/chat/completions" appended after it.
func NormalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("base_url must be an http(s) URL, got %q", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("base_url must not contain credentials, a query or a fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
