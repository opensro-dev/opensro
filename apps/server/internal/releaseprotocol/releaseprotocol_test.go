/*
===========================================================================

releaseprotocol_test.go - one protocol, refused otherwise

===========================================================================
*/
package releaseprotocol

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

/*
================
serve

Sends one request through Require and reports whether it reached the
handler, with the recorded response.
================
*/
func serve(t *testing.T, method, declared string) (bool, *httptest.ResponseRecorder) {
	t.Helper()
	reached := false
	handler := Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	request := httptest.NewRequest(method, "/title/login", nil)
	if declared != "" {
		request.Header.Set(Header, declared)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return reached, recorder
}

/*
================
TestTheCurrentProtocolPasses
================
*/
func TestTheCurrentProtocolPasses(t *testing.T) {
	if reached, recorder := serve(t, http.MethodPost, strconv.Itoa(Current)); !reached {
		t.Fatalf("current protocol refused with %d", recorder.Code)
	}
}

/*
================
TestAnyOtherDeclarationIsRefusedAsOutdated

Browsers older than the handshake declare nothing; a stale or garbled
declaration is no better. Each is told which protocol the server speaks.
================
*/
func TestAnyOtherDeclarationIsRefusedAsOutdated(t *testing.T) {
	for _, declared := range []string{"", strconv.Itoa(Current - 1), strconv.Itoa(Current + 1), "three"} {
		reached, recorder := serve(t, http.MethodGet, declared)
		if reached || recorder.Code != http.StatusUpgradeRequired {
			t.Fatalf("declared %q: reached %v, status %d; want refused with 426", declared, reached, recorder.Code)
		}
		var body struct {
			Error    string `json:"error"`
			Protocol int    `json:"protocol"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error != "client-outdated" || body.Protocol != Current {
			t.Fatalf("declared %q: body %+v, want client-outdated and protocol %d", declared, body, Current)
		}
	}
}

/*
================
TestCORSPreflightPasses

A preflight carries no declaration; refusing it would hide the 426 of the
real request behind a CORS failure.
================
*/
func TestCORSPreflightPasses(t *testing.T) {
	if reached, _ := serve(t, http.MethodOptions, ""); !reached {
		t.Fatal("CORS preflight was refused")
	}
}

/*
================
TestEveryReleasedProtocolNamesDistinctContracts
================
*/
func TestEveryReleasedProtocolNamesDistinctContracts(t *testing.T) {
	current, ok := ContractsOf(Current)
	if !ok || current != (Contracts{Bootstrap: BootstrapContract, Roster: RosterContract, References: ReferencesContract, Companions: CompanionsContract}) {
		t.Fatalf("protocol %d names %+v, want the current contract constants", Current, current)
	}
	seen := map[Contracts]int{}
	for protocol, contracts := range history {
		if previous, ok := seen[contracts]; ok {
			t.Fatalf("protocols %d and %d name the same contracts %+v", previous, protocol, contracts)
		}
		seen[contracts] = protocol
	}
}
