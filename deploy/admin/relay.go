/*
===========================================================================

relay.go - the routes the panel may reach in another container

The Agent's provisioning API and every GameWorld's player operations
listen on their own loopback only, and the panel lives in one container's
namespace. A relay runs in the other's and forwards exactly the routes
the panel uses: the account routes (relayAccounts) or the player
operations (relayOperator). Everything else is refused, so the block
routes and anything the upstream adds later stay unreachable from the
network. It forwards no client address: the target sees a loopback peer
and still demands the token the panel sends.

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

// The route sets a relay forwards (RELAY_ROUTES).
const (
	relayAccounts = "accounts"
	relayOperator = "operator"
)

// operatorPath is the GameWorld's player operations (agentapi operator.go).
const operatorPath = "/internal/operations/player"

/*
================
operatorAllows

GET reads a character, POST runs one operation; nothing else.
================
*/
func operatorAllows(method, path string) bool {
	return path == operatorPath && (method == http.MethodGet || method == http.MethodPost)
}

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
func newRelay(routes, target string) (http.Handler, error) {
	upstream, err := url.Parse(target)
	if err != nil || upstream.Scheme != "http" || upstream.Host == "" {
		return nil, fmt.Errorf("relay target %q must be an http URL", target)
	}
	allows := relayAllows
	switch routes {
	case relayAccounts:
	case relayOperator:
		allows = operatorAllows
	default:
		return nil, fmt.Errorf("relay routes %q must be %s or %s", routes, relayAccounts, relayOperator)
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(request *httputil.ProxyRequest) {
		request.SetURL(upstream)
	}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allows(r.Method, r.URL.EscapedPath()) {
			http.Error(w, "route not relayed", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, relayBodyLimit)
		proxy.ServeHTTP(w, r)
	}), nil
}
