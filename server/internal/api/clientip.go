package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// defaultTrustedProxies is used when TRUSTED_PROXY_CIDRS is empty: loopback only. The deployment
// sets the compose gateway range, because the host's nginx reaches the container through it.
const defaultTrustedProxies = "127.0.0.0/8,::1/128"

// ParseTrustedProxies reads a comma-separated list of CIDRs (a bare address is a single host).
// An empty list means defaultTrustedProxies.
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	if strings.TrimSpace(list) == "" {
		list = defaultTrustedProxies
	}
	var out []netip.Prefix
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			a, aerr := netip.ParseAddr(part)
			if aerr != nil {
				return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS: %q is not a CIDR or an address", part)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// clientIP returns the address of the client of r. X-Real-IP is believed only when the direct
// peer is inside trusted (the host's nginx always overwrites it); otherwise, and when the header
// is missing or not an address, it is the peer itself. CF-Connecting-IP and X-Forwarded-For are
// never read: the first is not cleared by the proxy, the second starts with the client's own value.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	peer := peerAddr(r.RemoteAddr)
	if !peer.IsValid() {
		return r.RemoteAddr
	}
	for _, p := range trusted {
		if p.Contains(peer) {
			if real, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
				return real.Unmap().String()
			}
			break
		}
	}
	return peer.String()
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}
