/*
===========================================================================

client_ip_test.go - the forwarded client is trusted only from the edge

The Agent listens on loopback behind the same host's TLS edge, so its
peer is the edge and the player is the first X-Forwarded-For entry the
edge recorded. A network peer's header is its own claim and is never
believed (#2716 proxy proof, Agent half; the transport half is
clientaddr_test.go, and the two rules are kept in step on purpose).
===========================================================================
*/
package agentserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
================
TestClientIPTrustsForwardingOnlyFromTheEdge
================
*/
func TestClientIPTrustsForwardingOnlyFromTheEdge(t *testing.T) {
	for _, row := range []struct {
		name      string
		peer      string
		forwarded string
		realIP    string
		want      string
	}{
		{"edge forwards the client", "127.0.0.1:41000", "203.0.113.9, 10.0.0.1", "", "203.0.113.9"},
		{"edge falls back to real ip", "127.0.0.1:41000", "", "203.0.113.10", "203.0.113.10"},
		{"edge without headers names itself", "127.0.0.1:41000", "", "", "127.0.0.1"},
		{"forwarded garbage is not an address", "127.0.0.1:41000", "not-an-ip", "", "127.0.0.1"},
		{"a network peer cannot forge its address", "203.0.113.1:5000", "198.51.100.7", "", "203.0.113.1"},
		{"a direct client is its own address", "203.0.113.1:5000", "", "", "203.0.113.1"},
		{"ipv6 edge forwards an ipv6 client", "[::1]:41000", "2001:db8::7", "", "2001:db8::7"},
	} {
		t.Run(row.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/title/login", nil)
			request.RemoteAddr = row.peer
			if row.forwarded != "" {
				request.Header.Set("X-Forwarded-For", row.forwarded)
			}
			if row.realIP != "" {
				request.Header.Set("X-Real-IP", row.realIP)
			}
			if got := clientIP(request); got != row.want {
				t.Fatalf("clientIP = %q, want %q", got, row.want)
			}
		})
	}
}
