/*
===========================================================================

ticket_pace_test.go - a looping client is refused tickets past its allowance

===========================================================================
*/

package agentapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

/*
================
mintStatus

POSTs one mint and returns its status, Retry-After and body code.
================
*/
func mintStatus(t *testing.T, handler http.Handler, path string, body any) (int, string, string) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var answer struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &answer)
	return recorder.Code, recorder.Header().Get("Retry-After"), answer.Code
}

/*
================
issuedTicketCount

How many tickets the revocation ledger holds for the account: every
minted ticket is recorded there, so a refusal must leave it unchanged.
================
*/
func issuedTicketCount(api *API, accountID string) int {
	api.revocations.mu.Lock()
	defer api.revocations.mu.Unlock()
	return len(api.revocations.issuedTickets[accountID])
}

/*
================
TestTransportTicketsArePacedPerAccount

Past the burst the transport mint answers 429 RATE_LIMITED with a
Retry-After and mints nothing; another account keeps its own
allowance; a refill period hands out one more.
================
*/
func TestTransportTicketsArePacedPerAccount(t *testing.T) {
	api, _ := newTestAPI(t)
	clock := api.now()
	api.now = func() time.Time { return clock }
	alice := authenticatedHandler(t, api, "alice")
	bob := authenticatedHandler(t, api, "bob")

	for i := range transportTicketBurst {
		if code, _, _ := mintStatus(t, alice, "/auth/transport-token", map[string]any{}); code != http.StatusOK {
			t.Fatalf("ticket %d inside the burst = %d", i, code)
		}
	}
	issued := issuedTicketCount(api, "alice")
	for range 100 {
		code, retry, reason := mintStatus(t, alice, "/auth/transport-token", map[string]any{})
		if code != http.StatusTooManyRequests || reason != ticketRateLimitedCode || retry != "5" {
			t.Fatalf("past the burst = %d %q Retry-After %q, want 429 %s 5", code, reason, retry, ticketRateLimitedCode)
		}
	}
	if got := issuedTicketCount(api, "alice"); got != issued {
		t.Fatalf("refusals recorded tickets: %d, want %d", got, issued)
	}
	if code, _, _ := mintStatus(t, bob, "/auth/transport-token", map[string]any{}); code != http.StatusOK {
		t.Fatalf("another account = %d, want its own allowance", code)
	}
	clock = clock.Add(transportTicketRefill)
	if code, _, _ := mintStatus(t, alice, "/auth/transport-token", map[string]any{}); code != http.StatusOK {
		t.Fatalf("after a refill = %d", code)
	}
	if code, _, _ := mintStatus(t, alice, "/auth/transport-token", map[string]any{}); code != http.StatusTooManyRequests {
		t.Fatalf("second ticket after one refill = %d", code)
	}
}

/*
================
TestEnterWorldTicketsArePacedBeforeTheCharacterRead

The EnterWorld mint refuses past its burst before it reads the
character: a name that does not exist still answers 429, not
UNKNOWN_CHARACTER, once the allowance is spent.
================
*/
func TestEnterWorldTicketsArePacedBeforeTheCharacterRead(t *testing.T) {
	api, _ := newTestAPI(t)
	clock := api.now()
	api.now = func() time.Time { return clock }
	handler := authenticatedHandler(t, api, "")
	postJSON(t, handler, "/character/create", createBody("PaceHero"))

	body := map[string]any{"characterName": "PaceHero"}
	for i := range enterWorldTicketBurst {
		if code, _, _ := mintStatus(t, handler, "/auth/enterworld-token", body); code != http.StatusOK {
			t.Fatalf("ticket %d inside the burst = %d", i, code)
		}
	}
	code, retry, reason := mintStatus(t, handler, "/auth/enterworld-token", map[string]any{"characterName": "Nobody"})
	if code != http.StatusTooManyRequests || reason != ticketRateLimitedCode || retry != "10" {
		t.Fatalf("past the burst = %d %q Retry-After %q, want 429 %s 10", code, reason, retry, ticketRateLimitedCode)
	}
	clock = clock.Add(enterWorldTicketRefill)
	if code, _, _ := mintStatus(t, handler, "/auth/enterworld-token", body); code != http.StatusOK {
		t.Fatalf("after a refill = %d", code)
	}
}
