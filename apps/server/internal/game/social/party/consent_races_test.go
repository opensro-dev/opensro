/*
===========================================================================

consent_races_test.go - real-wire failures after party proposal admission.

Change authoritative membership after the prompt, then verify both reply
carriers and that a rejected or duplicate acceptance cannot add membership.

===========================================================================
*/
package party_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
TestPartyConsentInvalidationFeedback
================
*/
func TestPartyConsentInvalidationFeedback(t *testing.T) {
	for _, scenario := range []string{"join-offline", "join-privilege", "join-full", "form-target-partied"} {
		t.Run(scenario, func(t *testing.T) {
			server := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
			a := dialWS(t, server.srv)
			helloWS(t, a)
			enterWorld(t, a, e2eNameA)
			b := dialWS(t, server.srv)
			helloWS(t, b)
			enterWorld(t, b, e2eNameB)
			c := dialWS(t, server.srv)
			helloWS(t, c)
			enterWorld(t, c, e2eNameC)
			registry := server.runtime.Registry()
			memberA := party.Member{MemberID: gidA, Name: e2eNameA}
			memberB := party.Member{MemberID: gidB, Name: e2eNameB}
			memberC := party.Member{MemberID: gidC, Name: e2eNameC}
			ack := party.OpPartyJoinInviteAck
			if scenario == "form-target-partied" {
				ack = party.OpCreatePartyAck
				sendFrame(t, a, party.OpPartyInviteRequest, inviteFrame(gidB, 0))
				expectPartyPrompt(t, b, gidA, "formation prompt", 2, 0)
				if _, reason := registry.Form(e2eDivision, memberB, memberC, 0); reason != "" {
					t.Fatal(reason)
				}
			} else {
				if _, reason := registry.Form(e2eDivision, memberA, memberC, 0); reason != "" {
					t.Fatal(reason)
				}
				sendFrame(t, a, party.OpPartyJoinInviteRequest, u32le(gidB))
				expectPartyPrompt(t, b, gidA, "join prompt", 3, 0)
			}
			code := byte(2)
			switch scenario {
			case "join-offline":
				sendFrame(t, a, transport.OpBye, []byte{transport.ByeReasonNormal})
				a.Close()
				waitForPresenceDrop(t, server, e2eNameA)
				// The unbind and the close hook's party drop are separate steps:
				// count the parties only once A's membership is gone.
				wait.Eventually(t, 5*time.Second, "the offline inviter's party to dissolve", func() bool {
					_, joined := registry.PartyOf(e2eDivision, e2eNameA)
					return !joined
				})
				code = 14
			case "join-privilege":
				if _, reason := registry.Leave(e2eDivision, e2eNameA); reason != "" {
					t.Fatal(reason)
				}
				if _, reason := registry.Form(e2eDivision, memberC, memberA, 0); reason != "" {
					t.Fatal(reason)
				}
			case "join-full":
				for id := uint32(100); id < 102; id++ {
					if _, reason := registry.Join(e2eDivision, e2eNameA, party.Member{MemberID: id, Name: fmt.Sprint("Occupant", id)}); reason != "" {
						t.Fatal(reason)
					}
				}
			}
			before := registry.Count()
			sendFrame(t, b, party.OpInvitationProposal, consentFrame(1))
			expectExactFrame(t, b, party.OpPartyJoinAck, []byte{2, code}, "invitee terminal failure")
			if scenario != "join-offline" {
				expectExactFrame(t, a, ack, []byte{2, code}, "proposer terminal failure")
			}
			if registry.Count() != before || registry.PendingInviteCount() != 0 {
				t.Fatal("failed acceptance changed party authority")
			}
			if snapshot, joined := registry.PartyOf(e2eDivision, e2eNameB); joined && snapshot.LeaderID != gidB {
				t.Fatal("rejected invitee joined inviter")
			}
			sendFrame(t, b, party.OpInvitationProposal, consentFrame(1))
			gameReadyBarrier(t, b, "duplicate acceptance stays silent")
			if scenario != "join-offline" {
				gameReadyBarrier(t, a, "no repeated proposer failure")
			}
		})
	}
}
