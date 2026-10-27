/*
===========================================================================

towneffects_test.go - the town teleport head: reciprocal hostility
settlement, the penalty state's own-list-only clear, and the abnormal
clear on a completed return scroll

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
townHeadFixture

Two characters in one division runtime, for the reciprocal-erasure
tests.
================
*/
func townHeadFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, *fakeClock) {
	t.Helper()
	a := rebirthTestCharacter(20, 100)
	b := rebirthTestCharacter(20, 100)
	b.Name = "TownHeadPeer"
	b.ID = a.ID + 1000
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {a, b}},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	return rt, a, b, clock
}

/*
================
TestTownTeleportHeadErasesReciprocalAggressions
================
*/
func TestTownTeleportHeadErasesReciprocalAggressions(t *testing.T) {
	rt, a, b, _ := townHeadFixture(t)
	gidA := enterworld.ObjectIDForCharacter(a)
	gidB := enterworld.ObjectIDForCharacter(b)
	a.Aggressions = map[uint32]uint32{gidB: playerAggressionTicks}
	b.Aggressions = map[uint32]uint32{gidA: playerAggressionTicks}
	if a.PVPState() != 1 || b.PVPState() != 1 {
		t.Fatal("fixture is not mutually grey")
	}
	var peerFrames []wire.Frame
	rt.PushCharacterFrames = func(_, name string, f []wire.Frame) {
		if name == b.Name {
			peerFrames = append(peerFrames, f...)
		}
	}
	rt.PushDivisionPeerFrames = func(_, _ string, f []wire.Frame) {}
	actor, public := rt.applyTownTeleportHead(testDivision, a)
	if len(a.Aggressions) != 0 || len(b.Aggressions) != 0 {
		t.Fatalf("aggressions survive: %v / %v", a.Aggressions, b.Aggressions)
	}
	if a.PVPState() != 0 || b.PVPState() != 0 {
		t.Fatalf("states a=%d b=%d", a.PVPState(), b.PVPState())
	}
	if len(actor) == 0 || actor[0].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("actor frames %+v", actor)
	}
	if len(public) == 0 || public[0].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("public frames %+v", public)
	}
	if len(peerFrames) == 0 || peerFrames[0].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("peer was not told its state cleared: %+v", peerFrames)
	}
}

/*
================
TestTownTeleportHeadPenaltyClearsOwnListOnly
================
*/
func TestTownTeleportHeadPenaltyClearsOwnListOnly(t *testing.T) {
	rt, a, b, _ := townHeadFixture(t)
	gidA := enterworld.ObjectIDForCharacter(a)
	gidB := enterworld.ObjectIDForCharacter(b)
	a.PK = &domain.PKRecord{Penalty: 200000}
	a.Aggressions = map[uint32]uint32{gidB: playerAggressionTicks}
	b.Aggressions = map[uint32]uint32{gidA: playerAggressionTicks}
	if a.PVPState() != 2 {
		t.Fatal("fixture is not a murderer")
	}
	rt.applyTownTeleportHead(testDivision, a)
	if len(a.Aggressions) != 0 {
		t.Fatalf("penalty list survives: %v", a.Aggressions)
	}
	if _, ok := b.Aggressions[gidA]; !ok {
		t.Fatal("penalty arm erased the reciprocal record")
	}
	if a.PVPState() != 2 {
		t.Fatalf("penalty state moved: %d", a.PVPState())
	}
}

/*
================
TestTownTeleportHeadErasesOneSidedRecords

Aggression records are one-sided: the attacker retains a target that
never retained it back. The state-1 head erases the teleporting
character from every list in the division that names it, not only from
the lists of the characters it attacked itself.
================
*/
func TestTownTeleportHeadErasesOneSidedRecords(t *testing.T) {
	target := rebirthTestCharacter(20, 100)
	attacker := rebirthTestCharacter(20, 100)
	attacker.Name = "TownHeadAttacker"
	attacker.ID = target.ID + 1000
	other := rebirthTestCharacter(20, 100)
	other.Name = "TownHeadOther"
	other.ID = target.ID + 2000
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {target, attacker, other}},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	gidTarget := enterworld.ObjectIDForCharacter(target)
	// The target is grey through the third character; the attacker holds
	// the only record naming the target and is grey through it alone.
	target.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(other): playerAggressionTicks}
	attacker.Aggressions = map[uint32]uint32{gidTarget: playerAggressionTicks}
	if target.PVPState() != 1 || attacker.PVPState() != 1 {
		t.Fatal("fixture is not grey on both sides")
	}
	var attackerFrames []wire.Frame
	rt.PushCharacterFrames = func(_, name string, f []wire.Frame) {
		if name == attacker.Name {
			attackerFrames = append(attackerFrames, f...)
		}
	}
	rt.PushDivisionPeerFrames = func(_, _ string, f []wire.Frame) {}
	rt.applyTownTeleportHead(testDivision, target)
	if len(attacker.Aggressions) != 0 || attacker.PVPState() != 0 {
		t.Fatalf("one-sided record survives: %v state %d", attacker.Aggressions, attacker.PVPState())
	}
	if len(attackerFrames) == 0 || attackerFrames[0].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("attacker was not told its state cleared: %+v", attackerFrames)
	}
	if len(target.Aggressions) != 0 || target.PVPState() != 0 {
		t.Fatalf("target survives: %v state %d", target.Aggressions, target.PVPState())
	}
}

/*
================
TestPresentPointRebirthAppliesTownHead

The head runs before either arm of the native routine: a present-point
rebirth settles hostility (including the one-sided record another
character holds) exactly as an appointed-point arrival does.
================
*/
func TestPresentPointRebirthAppliesTownHead(t *testing.T) {
	c := rebirthTestCharacter(1, 0)
	peer := rebirthTestCharacter(1, 100)
	peer.Name = "TownHeadPeer"
	peer.ID = c.ID + 1000
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {c, peer}},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	rt.PushCharacterFrames = func(_, _ string, f []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, f []wire.Frame) {}
	gidC := enterworld.ObjectIDForCharacter(c)
	c.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(peer): playerAggressionTicks}
	peer.Aggressions = map[uint32]uint32{gidC: playerAggressionTicks}
	if c.PVPState() != 1 {
		t.Fatal("fixture is not grey")
	}
	out := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint})
	if len(c.Aggressions) != 0 || c.PVPState() != 0 {
		t.Fatalf("hostility survives a present-point rebirth: %v state %d", c.Aggressions, c.PVPState())
	}
	if _, ok := peer.Aggressions[gidC]; ok || peer.PVPState() != 0 {
		t.Fatalf("one-sided record survives a present-point rebirth: %v state %d", peer.Aggressions, peer.PVPState())
	}
	found := false
	for _, frame := range out.Frames {
		if frame.Opcode == wire.OpObjectStateRefresh {
			found = true
		}
	}
	if !found {
		t.Fatalf("no PvP state frame in the rebirth publication: %+v", out.Frames)
	}
}

/*
================
TestReturnScrollCompletionAppliesTownHead

A completed return scroll is a town teleport: an active abnormal state
and a grey aggression both settle on arrival.
================
*/
func TestReturnScrollCompletionAppliesTownHead(t *testing.T) {
	rt, c, clock, _ := returnFixture(t, 5000)
	start := clock.NowMs()
	record := abnormal.Record{Status: abnormal.Panic, Grade: 1, DurationMs: 60000, SourceGID: enterworld.ObjectIDForCharacter(c), Param34: 50}
	rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{record}, start)
	if block := rt.playerAbnormal(testDivision, c.Name); block == nil || block.Mask == 0 {
		t.Fatal("abnormal fixture did not take")
	}
	c.Aggressions = map[uint32]uint32{424242: playerAggressionTicks}
	if c.PVPState() != 1 {
		t.Fatal("aggression fixture is not grey")
	}
	useReturn(rt, c)
	clock.Advance(6 * time.Second)
	rt.TickHook()(clock.NowMs())
	if c.NativeTeleportMode != 0 {
		t.Fatal("return scroll did not complete")
	}
	if block := rt.playerAbnormal(testDivision, c.Name); block != nil && block.Mask != 0 {
		t.Fatalf("abnormal mask %#x survives the town arrival", block.Mask)
	}
	if len(c.Aggressions) != 0 || c.PVPState() != 0 {
		t.Fatalf("hostility survives the town arrival: %v state %d", c.Aggressions, c.PVPState())
	}
}
