/*
===========================================================================

jobparty_e2e_test.go - opposing job users cannot party together

ShardManager 44ED70 (forming) and 44F130 (joining) refuse two players
whose active job classes may not party (44ED20) with 0x2C23: traders and
hunters together, thieves only with thieves, players outside job mode only
with each other.

===========================================================================
*/
package party_test

import (
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/party"
)

// jobSuitTypeFlags is a 3.1.7.<job> suit's packed type word.
func jobSuitTypeFlags(job uint8) uint16 { return 0x3ac | uint16(job)<<11 }

/*
================
dressForJob

Puts a job suit in the character's socket 8 before it enters the world.
================
*/
func dressForJob(t *testing.T, server e2eServer, name string, job uint8) {
	t.Helper()
	for _, c := range server.authority.Characters().CharactersForDivision(e2eDivision) {
		if !strings.EqualFold(c.Name, name) {
			continue
		}
		server.authority.MutateCharacter(c, "test-job-suit", func() {
			c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{
				Slot: enterworld.JobSuitSlot, RefObjID: 90000 + uint32(job),
				Codename: "ITEM_TEST_JOB_SUIT", TypeFlags: jobSuitTypeFlags(job), StackCount: 1,
			})
		})
		return
	}
	t.Fatalf("no character %s to dress", name)
}

/*
================
TestOpposingJobsCannotFormOrJoinAParty
================
*/
func TestOpposingJobsCannotFormOrJoinAParty(t *testing.T) {
	server := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
	dressForJob(t, server, e2eNameA, domain.JobTrader)
	dressForJob(t, server, e2eNameB, domain.JobThief)
	dressForJob(t, server, e2eNameC, domain.JobHunter)
	connA, connB, connC, connD := dialWS(t, server.srv), dialWS(t, server.srv), dialWS(t, server.srv), dialWS(t, server.srv)
	helloWS(t, connA)
	enterWorld(t, connA, e2eNameA)
	helloWS(t, connB)
	enterWorld(t, connB, e2eNameB)
	helloWS(t, connC)
	enterWorld(t, connC, e2eNameC)
	helloWS(t, connD)
	enterWorld(t, connD, e2eNameD)
	registry := server.runtime.Registry()

	// A trader proposes to a thief: the accepted proposal is refused with
	// 0x23 to both, and no party forms.
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectPartyPrompt(t, connB, gidA, "the thief's proposal", 2, 3)
	sendFrame(t, connB, party.OpInvitationProposal, consentFrame(1))
	expectExactFrame(t, connA, party.OpCreatePartyAck, []byte{2, 0x23}, "trader told the pair may not party")
	expectExactFrame(t, connB, party.OpPartyJoinAck, []byte{2, 0x23}, "thief told the pair may not party")
	if got := registry.Count(); got != 0 {
		t.Fatalf("a trader and a thief formed %d party(s)", got)
	}

	// A trader and a hunter party together.
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidC, 0x03))
	expectPartyPrompt(t, connC, gidA, "the hunter's proposal", 2, 3)
	sendFrame(t, connC, party.OpInvitationProposal, consentFrame(1))
	rows := []party.MemberRow{chinaMaleRow(gidA, e2eNameA), chinaMaleRow(gidC, e2eNameC)}
	expectPartySeed(t, connA, gidA, gidA, 0x03, rows, "trader's party")
	expectPartySeed(t, connC, gidC, gidA, 0x03, rows, "hunter's party")

	// A player outside job mode may not join the job party.
	sendFrame(t, connA, party.OpPartyJoinInviteRequest, u32le(gidD))
	expectPartyPrompt(t, connD, gidA, "the plain player's join proposal", 3, 3)
	sendFrame(t, connD, party.OpInvitationProposal, consentFrame(1))
	expectExactFrame(t, connA, party.OpPartyJoinInviteAck, []byte{2, 0x23}, "leader told the joiner may not party")
	expectExactFrame(t, connD, party.OpPartyJoinAck, []byte{2, 0x23}, "joiner told it may not party")
	if snapshot, ok := registry.PartyOf(e2eDivision, e2eNameA); !ok || len(snapshot.Members) != 2 {
		t.Fatalf("the job party is %+v (%v), want its two members", snapshot, ok)
	}
}

/*
================
TestMatchJoinCommitRefusesOpposingJobs

The match board's commit repeats 44F850's pair test: a thief cannot join a
trader's listing even once the owner has approved.
================
*/
func TestMatchJoinCommitRefusesOpposingJobs(t *testing.T) {
	server := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
	dressForJob(t, server, e2eNameA, domain.JobTrader)
	dressForJob(t, server, e2eNameB, domain.JobThief)
	dressForJob(t, server, e2eNameC, domain.JobHunter)
	connA, connB, connC := dialWS(t, server.srv), dialWS(t, server.srv), dialWS(t, server.srv)
	helloWS(t, connA)
	enterWorld(t, connA, e2eNameA)
	helloWS(t, connB)
	enterWorld(t, connB, e2eNameB)
	helloWS(t, connC)
	enterWorld(t, connC, e2eNameC)
	if reason := server.runtime.AdmitMatchJoin(e2eDivision, e2eNameA, e2eNameB, 0x03); reason != "opposing job classes" {
		t.Fatalf("a thief joining a trader's listing committed with %q", reason)
	}
	if got := server.runtime.Registry().Count(); got != 0 {
		t.Fatalf("the refused match join formed %d party(s)", got)
	}
	if reason := server.runtime.AdmitMatchJoin(e2eDivision, e2eNameA, e2eNameC, 0x03); reason != "" {
		t.Fatalf("a hunter joining a trader's listing was refused: %q", reason)
	}
}
