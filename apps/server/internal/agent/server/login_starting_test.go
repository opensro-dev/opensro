/*
===========================================================================

login_starting_test.go - login waits for a starting shard, never fails it

A GameWorld holds its lease ~50 s before it admits players. Login in that
window answers the starting code with a Retry-After at the login budget's
refill, so the client's starting wait retries instead of showing the shard
offline, and the retries never spend the budget.

===========================================================================
*/
package agentserver

import (
	"net/http"
	"testing"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/platform/readiness"
)

/*
================
TestLoginWaitsForAStartingShard
================
*/
func TestLoginWaitsForAStartingShard(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	loginBody := `{"id":"tester","password":"123123","serverId":"alpha"}`
	publish := func(sequence uint64, phase string) {
		t.Helper()
		if err := fixture.directory.Publish(shard.Heartbeat{
			ShardID: "alpha", InstanceID: "worker-a", Sequence: sequence, Phase: phase,
		}, *fixture.now); err != nil {
			t.Fatal(err)
		}
	}

	publish(1, shard.PhaseStarting)
	response := performJSON(t, fixture.handler, http.MethodPost, "/title/login", loginBody, "")
	body := decodeObject(t, response)
	if response.Code != http.StatusServiceUnavailable || body["code"] != readiness.CodeStarting {
		t.Fatalf("starting shard login = %d %#v; want 503 %s", response.Code, body, readiness.CodeStarting)
	}
	if got := response.Header().Get("Retry-After"); got != "6" || body["retryAfter"] != float64(6) {
		t.Fatalf("Retry-After %q / %v; want 6, the login budget's refill", got, body["retryAfter"])
	}

	// Players behind one address all wait through the start: the starting
	// answer is not charged to the per-address login budget (burst 10), so
	// twenty retries still leave the real login admissible.
	for i := 0; i < 20; i++ {
		response = performJSON(t, fixture.handler, http.MethodPost, "/title/login", loginBody, "")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("starting retry %d answered %d %#v; want 503 starting", i, response.Code, decodeObject(t, response))
		}
	}

	// Once the shard admits, the same login is accepted.
	publish(2, shard.PhaseAdmitting)
	response = performJSON(t, fixture.handler, http.MethodPost, "/title/login", loginBody, "")
	if body := decodeObject(t, response); body["ok"] != true {
		t.Fatalf("admitting shard login = %#v", body)
	}
}
