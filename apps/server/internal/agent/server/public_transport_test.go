/*
===========================================================================

public_transport_test.go - what clients are told about the game transport

The Agent advertises each shard's transport to clients in the server list
and in the login and reload-restore answers: the URL (a host, or an edge
route the web edge serving the client proxies) and whether it is a route,
so no client infers the form from the string (catalog.go
TransportEdgeRoute).

===========================================================================
*/

package agentserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
================
TestClientsReceivePublicTransportRoute
================
*/
func TestClientsReceivePublicTransportRoute(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	publishFixtureLease(t, fixture, "beta", "worker-b", 1, 0)
	want := map[string]string{"alpha": "https://127.0.0.1:9001", "beta": "/shards/beta"}

	var servers []serverInfo
	response := performJSON(t, fixture.handler, http.MethodGet, "/title/servers", "", "")
	if err := json.Unmarshal(response.Body.Bytes(), &servers); err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.TransportURL != want[server.ID] {
			t.Fatalf("directory advertises %q for %q, want %q", server.TransportURL, server.ID, want[server.ID])
		}
	}
	for id, transport := range want {
		login := performJSON(t, fixture.handler, http.MethodPost, "/title/login",
			`{"id":"tester","password":"123123","serverId":"`+id+`"}`, "")
		if body := decodeObject(t, login); body["ok"] != true || body["transportUrl"] != transport {
			t.Fatalf("login to %q = %s, want transportUrl %q", id, login.Body.String(), transport)
		}
		// The form is stated, not left for the client to read off the string:
		// a route carries no QUIC, so the client dials the socket alone.
		route := id == "beta"
		if body := decodeObject(t, login); body["transportRoute"] != route {
			t.Fatalf("login to %q = %s, want transportRoute %v", id, login.Body.String(), route)
		}
		cookies := login.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatalf("login to %q set no session cookie", id)
		}
		restore := httptest.NewRequest(http.MethodPost, "/title/session", strings.NewReader(`{}`))
		declareBrowser(restore)
		restore.Header.Set("Content-Type", "application/json")
		restore.AddCookie(cookies[0])
		restored := httptest.NewRecorder()
		fixture.handler.ServeHTTP(restored, restore)
		if body := decodeObject(t, restored); body["ok"] != true || body["transportUrl"] != transport ||
			body["transportRoute"] != route {
			t.Fatalf("restore of %q = %s, want transportUrl %q and transportRoute %v", id, restored.Body.String(), transport, route)
		}
	}
}
