/*
===========================================================================

relay.go - the account routes the panel may reach on the Agent

The Agent's provisioning API listens on its own loopback only (the
upstream refuses any other address), and the panel lives in another
container's namespace. The relay forwards exactly the routes the panel's
account page uses and refuses the rest, so the block routes and anything
the upstream adds later stay unreachable from the network. It forwards no
client address: the Agent sees a loopback peer and still demands the
provisioning token the panel sends.

===========================================================================
*/
package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// relayBodyLimit matches the provisioning API's own request limit.
const relayBodyLimit = 4 << 10

/*
================
relayAllows

POST /v1/accounts, GET /v1/accounts/{id}, PUT /v1/accounts/{id}/password
and PUT /v1/accounts/{id}/disabled.
================
*/
func relayAllows(method, path string) bool {
	if method == http.MethodPost && path == "/v1/accounts" {
		return true
	}
	rest, found := strings.CutPrefix(path, "/v1/accounts/")
	if !found || rest == "" {
		return false
	}
	id, sub, _ := strings.Cut(rest, "/")
	if id == "" {
		return false
	}
	switch sub {
	case "":
		return method == http.MethodGet
	case "password", "disabled":
		return method == http.MethodPut
	}
	return false
}

/*
================
newRelay
================
*/
func newRelay(target string) (http.Handler, error) {
	upstream, err := url.Parse(target)
	if err != nil || upstream.Scheme != "http" || upstream.Host == "" {
		return nil, fmt.Errorf("relay target %q must be an http URL", target)
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(request *httputil.ProxyRequest) {
		request.SetURL(upstream)
	}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !relayAllows(r.Method, r.URL.EscapedPath()) {
			http.Error(w, "route not relayed", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, relayBodyLimit)
		proxy.ServeHTTP(w, r)
	}), nil
}
