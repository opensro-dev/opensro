/*
===========================================================================

clientaddr_test.go - the forwarded client is trusted only from the edge

Behind the production edge (Caddy on the same host, loopback listeners)
the peer of every WebSocket is the edge; the player is the first
X-Forwarded-For entry it recorded. A network peer's header is its own
claim and is never believed (#2716 proxy proof, WS/WT half).
===========================================================================
*/
package transport

import (
	"net/http"
	"testing"
)

/*
================
TestClientAddrTrustsForwardingOnlyFromTheEdge
================
*/
func TestClientAddrTrustsForwardingOnlyFromTheEdge(t *testing.T) {
	header := func(values map[string]string) http.Header {
		h := http.Header{}
		for name, value := range values {
			h.Set(name, value)
		}
		return h
	}
	for _, row := range []struct {
		name   string
		peer   string
		header http.Header
		want   string
	}{
		{"edge forwards the client", "127.0.0.1:41000",
			header(map[string]string{"X-Forwarded-For": "203.0.113.9, 10.0.0.1"}), "203.0.113.9"},
		{"edge falls back to real ip", "127.0.0.1:41000",
			header(map[string]string{"X-Real-IP": "203.0.113.10"}), "203.0.113.10"},
		{"edge without headers names itself", "127.0.0.1:41000", header(nil), "127.0.0.1"},
		{"forwarded garbage is not an address", "127.0.0.1:41000",
			header(map[string]string{"X-Forwarded-For": "not-an-ip"}), "127.0.0.1"},
		{"a network peer cannot forge its address", "203.0.113.1:5000",
			header(map[string]string{"X-Forwarded-For": "198.51.100.7"}), "203.0.113.1"},
		{"a direct client is its own address", "203.0.113.1:5000", header(nil), "203.0.113.1"},
		{"ipv6 edge forwards an ipv6 client", "[::1]:41000",
			header(map[string]string{"X-Forwarded-For": "2001:db8::7"}), "2001:db8::7"},
		{"a portless peer still resolves", "203.0.113.1", header(nil), "203.0.113.1"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := clientAddr(row.peer, row.header); got != row.want {
				t.Fatalf("clientAddr(%q) = %q, want %q", row.peer, got, row.want)
			}
		})
	}
}
