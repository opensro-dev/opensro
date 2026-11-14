/*
===========================================================================

leaderhandover_e2e_test.go - the lead passes on when the leader goes (#3438)

A leader that disconnects or leaves a party of three or more does not
break it: every remaining member gets the leader's type-3 leave line, then
the type-9 LEADER DELEGATE naming the longest-standing member, who now
leads (the leader-only banish works for them).

===========================================================================
*/
package party_test

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/transport"
)

/*
================
TestPartyLeaderDepartureHandsTheLeadOverWire
================
*/
func TestPartyLeaderDepartureHandsTheLeadOverWire(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason uint8
	}{{"disconnect", party.PartyLeaveReasonLogout}, {"leave", party.PartyLeaveReasonSecede}} {
		t.Run(tc.name, func(t *testing.T) {
			server := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
			connA := dialWS(t, server.srv)
			helloWS(t, connA)
			enterWorld(t, connA, e2eNameA)
			connB := dialWS(t, server.srv)
			helloWS(t, connB)
			enterWorld(t, connB, e2eNameB)
			connC := dialWS(t, server.srv)
			helloWS(t, connC)
			enterWorld(t, connC, e2eNameC)
			registry := server.runtime.Registry()
			if _, reason := registry.Form(e2eDivision, party.Member{MemberID: gidA, Name: e2eNameA}, party.Member{MemberID: gidB, Name: e2eNameB}, 0); reason != "" {
				t.Fatal(reason)
			}
			if _, reason := registry.Join(e2eDivision, e2eNameA, party.Member{MemberID: gidC, Name: e2eNameC}); reason != "" {
				t.Fatal(reason)
			}

			if tc.reason == party.PartyLeaveReasonLogout {
				sendFrame(t, connA, transport.OpBye, []byte{transport.ByeReasonNormal})
				connA.Close()
				waitForPresenceDrop(t, server, e2eNameA)
			} else {
				sendFrame(t, connA, party.OpPartyLeaveRequest, nil)
				expectExactFrame(t, connA, party.OpPartyUpdate, party.EncodePartyLeave3E58(gidA, tc.reason), "A's own leave")
			}
			leave := party.EncodePartyLeave3E58(gidA, tc.reason)
			delegate := party.EncodePartyDelegate3E58(gidB)
			expectExactFrame(t, connB, party.OpPartyUpdate, leave, "B's leave line")
			expectExactFrame(t, connB, party.OpPartyUpdate, delegate, "B's delegation")
			expectExactFrame(t, connC, party.OpPartyUpdate, leave, "C's leave line")
			expectExactFrame(t, connC, party.OpPartyUpdate, delegate, "C's delegation")

			snapshot, ok := registry.PartyOf(e2eDivision, e2eNameC)
			if !ok || snapshot.LeaderID != gidB || len(snapshot.Members) != 2 {
				t.Fatalf("party after the handover = %+v (ok=%v), want B leading B and C", snapshot, ok)
			}

			// The new leader's leader-only banish works.
			sendFrame(t, connB, party.OpPartyBanishRequest, u32le(gidC))
			booted := party.EncodePartyLeave3E58(gidC, party.PartyLeaveReasonBooted)
			expectExactFrame(t, connC, party.OpPartyUpdate, booted, "C's BOOTED push")
			expectExactFrame(t, connB, party.OpPartyUpdate, booted, "B's boot fan-out")
		})
	}
}
