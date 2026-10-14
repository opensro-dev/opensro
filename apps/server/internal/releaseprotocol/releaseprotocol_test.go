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
	if !ok || current != (Contracts{Bootstrap: BootstrapContract, Roster: RosterContract, References: ReferencesContract, Companions: CompanionsContract,
		Effects: EffectsContract, Friends: FriendsContract, Notices: NoticesContract}) {
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

/*
================
TestAPreUncappedCompanionClientIsRefused

A protocol-6 browser refuses a COS bag over 140 slots inside its decoder
(#801 lifted that bound on the server). It must be refused with 426 and
told to reload before it can reach a decoder that would end its session.
================
*/
func TestAPreUncappedCompanionClientIsRefused(t *testing.T) {
	const uncappedBags = 2
	for protocol, contracts := range history {
		if contracts.Companions >= uncappedBags {
			continue
		}
		if reached, recorder := serve(t, http.MethodPost, strconv.Itoa(protocol)); reached || recorder.Code != http.StatusUpgradeRequired {
			t.Fatalf("protocol %d (companions %d): reached %v, status %d; want refused with 426",
				protocol, contracts.Companions, reached, recorder.Code)
		}
	}
	if CompanionsContract < uncappedBags {
		t.Fatalf("CompanionsContract = %d; this build encodes uncapped COS bags", CompanionsContract)
	}
}

/*
================
TestAClientWithoutOneShotRetirementIsRefused

A protocol-7 browser keeps every token-0 attached effect the server now
sends for a heal, cure or revival (no B6A0 ever ends one) and throws at
2048 rows, ending its session mid-play. It must be refused with 426 and
told to reload before it can reach that stream.
================
*/
func TestAClientWithoutOneShotRetirementIsRefused(t *testing.T) {
	const retiresOneShots = 1
	refused := 0
	for protocol, contracts := range history {
		if contracts.Effects >= retiresOneShots {
			continue
		}
		if reached, recorder := serve(t, http.MethodPost, strconv.Itoa(protocol)); reached || recorder.Code != http.StatusUpgradeRequired {
			t.Fatalf("protocol %d (effects %d): reached %v, status %d; want refused with 426",
				protocol, contracts.Effects, reached, recorder.Code)
		}
		refused++
	}
	if _, ok := history[7]; !ok || refused == 0 {
		t.Fatal("the protocol-7 client is not in the history this test refuses")
	}
	if EffectsContract < retiresOneShots {
		t.Fatalf("EffectsContract = %d; this build sends token-0 one-shots", EffectsContract)
	}
}

/*
================
TestAClientThatCannotAnswerAFriendRequestIsRefused

A protocol-8 browser ignores the target's friend prompt (S->C 0x7164) and
never sends the 0x3012 consent, so every request to it waits out the
window and cancels. It must be refused with 426 and told to reload.
================
*/
func TestAClientThatCannotAnswerAFriendRequestIsRefused(t *testing.T) {
	const answersPrompts = 1
	refused := 0
	for protocol, contracts := range history {
		if contracts.Friends >= answersPrompts {
			continue
		}
		if reached, recorder := serve(t, http.MethodPost, strconv.Itoa(protocol)); reached || recorder.Code != http.StatusUpgradeRequired {
			t.Fatalf("protocol %d (friends %d): reached %v, status %d; want refused with 426",
				protocol, contracts.Friends, reached, recorder.Code)
		}
		refused++
	}
	if _, ok := history[8]; !ok || refused == 0 {
		t.Fatal("the protocol-8 client is not in the history this test refuses")
	}
	if FriendsContract < answersPrompts {
		t.Fatalf("FriendsContract = %d; this build prompts friend request targets", FriendsContract)
	}
}

/*
================
TestAClientThatDropsTheNoticeChannelsIsRefused

A protocol-9 browser drops the port-only 0x3667 notice channels 0x80 (job
kills) and 0x81 (seal drops, +8..+11), so its player never sees them. It
must be refused with 426 and told to reload.
================
*/
func TestAClientThatDropsTheNoticeChannelsIsRefused(t *testing.T) {
	const showsNoticeChannels = 1
	refused := 0
	for protocol, contracts := range history {
		if contracts.Notices >= showsNoticeChannels {
			continue
		}
		if reached, recorder := serve(t, http.MethodPost, strconv.Itoa(protocol)); reached || recorder.Code != http.StatusUpgradeRequired {
			t.Fatalf("protocol %d (notices %d): reached %v, status %d; want refused with 426",
				protocol, contracts.Notices, reached, recorder.Code)
		}
		refused++
	}
	if _, ok := history[9]; !ok || refused == 0 {
		t.Fatal("the protocol-9 client is not in the history this test refuses")
	}
	if NoticesContract < showsNoticeChannels {
		t.Fatalf("NoticesContract = %d; this build sends the notice channels", NoticesContract)
	}
}
