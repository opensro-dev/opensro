/*
===========================================================================

skillobjects_seal_test.go - a skill object's EXP reaches a loading owner

A trap that kills commits its planter's EXP inside the skill-object pass.
The planter's client re-walks the level curve from every 0x30D2, so a
committed grant whose packet is lost leaves the client's mirror behind
and the next level crossing disconnects it ("Invalid experience update").
The tick's own session list holds only world-ready sessions; a planter
whose client is loading (a fresh entry, a teleport) must still receive the
grant, through the recipients captured at commit time (sealTickPrivate).

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// sealTestExpFrame marks the fake grant's 0x30D2 so the test can find it.
var sealTestExpFrame = wire.Frame{Opcode: wire.OpExpUpdate, Payload: wire.EncodeExpUpdate(0, 24, 0, false, 0)}

/*
================
lethalTrapFixture

A planted Fire Trap over a one-HP monster, with the EXP grant faked and
the commit-time capture recording what each character would be sent.
================
*/
func lethalTrapFixture(t *testing.T) (*Runtime, *enterworld.Character, int64, *[][]wire.Frame) {
	t.Helper()
	rt, clock, c, skill, _ := plantedTrapFixtureHP(t, "SKILL_EU_WIZARD_FIREA_TRAP_A_01", 1)
	rt.UpdateExperience = func(_ *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		if exp <= 0 {
			return nil, false
		}
		return []wire.Frame{sealTestExpFrame}, true
	}
	captured := &[][]wire.Frame{}
	owner := enterworld.ObjectIDForCharacter(c)
	rt.CaptureObjectFrames = func(_ string, gid uint32) func([]wire.Frame) {
		if gid != owner {
			return nil
		}
		return func(frames []wire.Frame) { *captured = append(*captured, frames) }
	}
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	return rt, c, release + int64(enterworld.CombatTrapScanMs), captured
}

/*
================
deliverToReadySessions

The ticker's routing (simulation routeHookFrames): a sealed batch runs its
captured delivery; a character-targeted batch reaches only a session in
the tick's world-ready list. Returns the frames the owner received.
================
*/
func deliverToReadySessions(out []simulation.DivisionFrames, ready map[int64]bool, captured *[][]wire.Frame, owner int64) []wire.Frame {
	var got []wire.Frame
	for _, batch := range out {
		if batch.Deliver != nil {
			batch.Deliver()
			continue
		}
		if batch.OnlyCharacterID == owner && ready[owner] {
			for _, f := range batch.Frames {
				got = append(got, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
			}
		}
	}
	for _, frames := range *captured {
		got = append(got, frames...)
	}
	return got
}

/*
================
hasExpUpdate
================
*/
func hasExpUpdate(frames []wire.Frame) bool {
	for _, f := range frames {
		if f.Opcode == wire.OpExpUpdate {
			return true
		}
	}
	return false
}

/*
================
TestTrapKillEXPReachesALoadingPlanter

The planter's client is still loading, so the tick lists no session for
it. The trap kills, the server commits the EXP, and the 0x30D2 must still
reach the planter's (loading) session.
================
*/
func TestTrapKillEXPReachesALoadingPlanter(t *testing.T) {
	rt, c, scan, captured := lethalTrapFixture(t)
	out := rt.AdvanceSkillObjects(scan, nil)
	if !hasExpUpdate(deliverToReadySessions(out, map[int64]bool{}, captured, c.ID)) {
		t.Fatal("a loading planter lost the trap kill's 0x30D2: its client's EXP mirror now trails the server")
	}
}

/*
================
TestTrapKillEXPStillReachesAReadyPlanter

The sealed route must not cost a world-ready planter its packet, nor send
it twice.
================
*/
func TestTrapKillEXPStillReachesAReadyPlanter(t *testing.T) {
	rt, c, scan, captured := lethalTrapFixture(t)
	sessions := []simulation.SessionView{{
		DivisionID: testDivision, CharacterID: c.ID, Population: rt.SkillObjects.Snapshot()[0].Population,
		PublishedObjects: []uint32{}, World: simulation.SeedWorldState(c),
	}}
	out := rt.AdvanceSkillObjects(scan, sessions)
	count := 0
	for _, f := range deliverToReadySessions(out, map[int64]bool{c.ID: true}, captured, c.ID) {
		if f.Opcode == wire.OpExpUpdate {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ready planter received %d 0x30D2, want exactly 1", count)
	}
}

/*
================
TestSkillObjectPassWaitsForAnEntryBootstrap

An entry bootstrap holds the division's publication gate from its snapshot
through its enqueue. A trap kill committed inside that span would be
missing from the snapshot and captured before the new session could
receive it, so the pass must wait for the gate.
================
*/
func TestSkillObjectPassWaitsForAnEntryBootstrap(t *testing.T) {
	rt, _, scan, _ := lethalTrapFixture(t)
	unlock := rt.LockPublication(testDivision, "test")
	done := make(chan struct{})
	go func() {
		rt.AdvanceSkillObjects(scan, nil)
		close(done)
	}()
	select {
	case <-done:
		unlock()
		t.Fatal("the skill-object pass committed while an entry bootstrap held the publication gate")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the skill-object pass never ran after the gate was released")
	}
}
