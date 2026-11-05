/*
===========================================================================

server_test.go - the transport/catalog boot guard (#2441)

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/transport"
)

/*
================
TestTransportCatalogPortMismatch

Only a port disagreement warns: hosts diverge on purpose (loopback
catalog against wildcard or private binds, TLS terminated in front),
while a bind port the catalog does not name advertises a port
nothing listens on.
================
*/
func TestTransportCatalogPortMismatch(t *testing.T) {
	def := func(transportURL string) shard.Definition {
		return shard.Definition{ID: "test", TransportURL: transportURL}
	}
	cfg := func(wt, ws string) transport.Config {
		return transport.Config{WTAddr: wt, WSAddr: ws}
	}
	cases := []struct {
		name string
		def  shard.Definition
		cfg  transport.Config
		want bool
	}{
		{"loopback match", def("http://127.0.0.1:8788"), cfg("127.0.0.1:8788", "127.0.0.1:8788"), false},
		{"wildcard bind, same port", def("http://127.0.0.1:8788"), cfg("0.0.0.0:8788", ":8788"), false},
		{"public host behind TLS, same port", def("https://play.example.com:8788"), cfg("127.0.0.1:8788", "127.0.0.1:8788"), false},
		{"https default port", def("https://play.example.com"), cfg("127.0.0.1:443", "127.0.0.1:443"), false},
		{"http default port", def("http://play.example.com"), cfg("127.0.0.1:80", "127.0.0.1:80"), false},
		{"one bind port adrift", def("http://127.0.0.1:8788"), cfg("127.0.0.1:8788", "127.0.0.1:9999"), true},
		{"both bind ports adrift", def("http://127.0.0.1:8788"), cfg("10.0.0.5:9001", "10.0.0.5:9002"), true},
		{"bind without a port", def("http://127.0.0.1:8788"), cfg("127.0.0.1", "127.0.0.1:8788"), true},
		{"invalid catalog URL stays silent", def("://bad"), cfg("127.0.0.1:1", "127.0.0.1:2"), false},
	}
	for _, tc := range cases {
		if got := transportCatalogPortMismatch(tc.def, tc.cfg); got != tc.want {
			t.Errorf("%s: mismatch = %v, want %v", tc.name, got, tc.want)
		}
	}
}
