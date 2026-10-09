package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientAddr returns the real client IP. Forwarding headers are only honored when the
// direct peer is a configured trusted proxy; the rightmost untrusted hop wins so clients
// can't spoof their address by prepending to X-Forwarded-For.
func (s *Server) clientAddr(r *http.Request) netip.Addr {
	peer := remoteAddr(r)
	if !peer.IsValid() || !s.cfg.IsTrustedProxy(peer) {
		return peer
	}
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		hops := strings.Split(strings.Join(xff, ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break
			}
			if !s.cfg.IsTrustedProxy(addr) {
				return addr.Unmap()
			}
		}
	}
	if addr, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-Ip"))); err == nil {
		return addr.Unmap()
	}
	return peer
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func (s *Server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := remoteAddr(r)
	return peer.IsValid() && s.cfg.IsTrustedProxy(peer) &&
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
