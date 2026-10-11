/*
===========================================================================

invite_range_e2e_test.go - the native reach gate on party proposals

CGObjPC_OnPartyFormRequest (5143B0) runs CGObjChar_CheckHitRange (4A8E10)
on the proposed member before any prompt exists. These tests move players
through the live-pose table the harness installs and check the refusal
codes over the wire.

===========================================================================
*/

package party_test

import (
	"path/filepath"
	"sync"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
e2ePoses

Live positions the invite range gate reads. Everyone stands together on
one outdoor spot unless a test moves them.
================
*/
type e2ePoses struct {
	mu    sync.Mutex
	moved map[string]simulation.Spawn
}

/*
================
e2ePoses.at
================
*/
func (p *e2ePoses) at(_ string, character *enterworld.Character) simulation.Spawn {
	p.mu.Lock()
	defer p.mu.Unlock()
	if spawn, ok := p.moved[character.Name]; ok {
		return spawn
	}
	return simulation.Spawn{RegionID: e2eRegion, X: 900, Y: 0, Z: 900}
}

/*
================
e2ePoses.move
================
*/
func (p *e2ePoses) move(name string, spawn simulation.Spawn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.moved[name] = spawn
}

// e2eRegion is an outdoor region (sector 0x48, 0x6A).
const e2eRegion uint16 = 0x6A48

/*
================
TestPartyInviteRangeGate

CGObjPC_OnPartyFormRequest (5143B0) runs CGObjChar_CheckHitRange (4A8E10)
on the proposed member: a player beyond 600 units answers code 4 (the
client's "too far"), one outside the adjacent sectors answers code 3
("invalid target"), and neither ever sees a prompt. A forged frame naming a
player in another town must not reach them.
================
*/
func TestPartyInviteRangeGate(t *testing.T) {
	first := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
	connA := dialWS(t, first.srv)
	helloWS(t, connA)
	enterWorld(t, connA, e2eNameA)
	connB := dialWS(t, first.srv)
	helloWS(t, connB)
	enterWorld(t, connB, e2eNameB)

	first.poses.move(e2eNameB, simulation.Spawn{RegionID: e2eRegion, X: 900 + 601, Y: 0, Z: 900})
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectExactFrame(t, connA, party.OpCreatePartyAck, []byte{2, 4}, "601 units away")

	first.poses.move(e2eNameB, simulation.Spawn{RegionID: e2eRegion + 2, X: 900, Y: 0, Z: 900})
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectExactFrame(t, connA, party.OpCreatePartyAck, []byte{2, 3}, "two sectors away")

	gameReadyBarrier(t, connB, "B saw no prompt")
	if got := first.runtime.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after range refusals = %d, want 0", got)
	}

	// Inside the range the proposal lands as before.
	first.poses.move(e2eNameB, simulation.Spawn{RegionID: e2eRegion, X: 900 + 599, Y: 0, Z: 900})
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectPartyPrompt(t, connB, gidA, "in range", 2, 3)
}

/*
================
setCharacter

Changes one stored character through the authority, as gameplay would.
================
*/
func setCharacter(t *testing.T, server e2eServer, name string, fn func(*enterworld.Character)) {
	t.Helper()
	for _, c := range server.authority.Characters().CharactersForDivision(e2eDivision) {
		if c.Name == name {
			server.authority.MutateCharacter(c, "test", func() { fn(c) })
			return
		}
	}
	t.Fatalf("no character %s", name)
}

/*
================
TestPartyProposerGate

CGObjPC_ValidatePartyFormation (513F90) runs before the target is read: a
proposer below level 5 answers 0x0A (the client's "too low creator level")
and a dead one 0x09; neither reaches the target.
================
*/
func TestPartyProposerGate(t *testing.T) {
	server := startPartyServer(t, filepath.Join(t.TempDir(), "authority"), true)
	connA := dialWS(t, server.srv)
	helloWS(t, connA)
	enterWorld(t, connA, e2eNameA)
	connB := dialWS(t, server.srv)
	helloWS(t, connB)
	enterWorld(t, connB, e2eNameB)

	setCharacter(t, server, e2eNameA, func(c *enterworld.Character) { level := int64(4); c.Level = &level })
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectExactFrame(t, connA, party.OpCreatePartyAck, []byte{2, 0x0A}, "level 4 proposer")

	setCharacter(t, server, e2eNameA, func(c *enterworld.Character) {
		level, hp := int64(5), int64(0)
		c.Level, c.CurrentHP = &level, &hp
	})
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectExactFrame(t, connA, party.OpCreatePartyAck, []byte{2, 0x09}, "dead proposer")

	gameReadyBarrier(t, connB, "B saw no prompt")
	if got := server.runtime.Registry().PendingInviteCount(); got != 0 {
		t.Fatalf("pending invites after proposer refusals = %d, want 0", got)
	}

	setCharacter(t, server, e2eNameA, func(c *enterworld.Character) { hp := int64(100); c.CurrentHP = &hp })
	sendFrame(t, connA, party.OpPartyInviteRequest, inviteFrame(gidB, 0x03))
	expectPartyPrompt(t, connB, gidA, "level 5 and alive", 2, 3)
}
