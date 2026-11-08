/*
===========================================================================

clientaddr.go - the real client address behind the local TLS edge

Production puts one TLS edge (Caddy) on the same host in front of the
loopback transport listeners, so a WebSocket's direct peer is the edge,
not the player. The edge records the client as the first
X-Forwarded-For entry. That header is trusted only from a loopback
peer - the rule the Agent's clientIP keeps for its routes
(internal/agent/server/server.go) - because any other peer could forge
its own identity for logs, limiters and the connection census (#2716).
Keeping the two rules in step is what makes the census's keyed address
hashes name the player on both halves of the server.

===========================================================================
*/
package transport

import (
	"net"
	"net/http"
	"strings"
)

/*
================
clientAddr

The client address of one accept: the first forwarded entry when the
direct peer is the loopback edge, else the peer itself. peer is the
connection's remote address ("host:port"); the answer is a bare IP.
WebTransport is dialed directly in the public posture, so its peer
already is the client and the headers are never consulted for it; the
same rule is applied so a future proxied WT accept behaves alike.
================
*/
func clientAddr(peer string, header http.Header) string {
	host, _, err := net.SplitHostPort(peer)
	if err != nil {
		host = peer
	}
	peerIP := net.ParseIP(host)
	if peerIP == nil || !peerIP.IsLoopback() {
		return host
	}
	if forwarded := strings.TrimSpace(header.Get("X-Forwarded-For")); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip.String()
		}
	}
	if realIP := net.ParseIP(strings.TrimSpace(header.Get("X-Real-IP"))); realIP != nil {
		return realIP.String()
	}
	return host
}
