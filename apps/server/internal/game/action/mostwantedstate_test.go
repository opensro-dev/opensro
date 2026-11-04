/*
===========================================================================

mostwantedstate_test.go - the wanted state frame, the lapse, the suit

Port-only, not native (#1849). The buff-board state (0x3FBA) follows a
counted kill, a raised level stamps the seven-day lapse, a lapsed
streak restarts from the next counted kill and pays nothing as a
victim, taking the suit off keeps the streak (only its bonuses shelve)
and the tick sweep sends a tracked holder its entry state, then clears
the streak when the lapse arrives. With SRO_MOST_WANTED off, no state
frame is ever sent.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/simulation"
)

// wantedState is one decoded 0x3FBA payload (mostwantedstate.go).
type wantedState struct {
	level int64
	into  int64
	job   int64
	per   int64
	lapse int64
}

/*
================
decodeWantedState

The last wanted state in a frame run, or false when none rode it.
================
*/
func decodeWantedState(frames []wire.Frame) (wantedState, bool) {
	for i := len(frames) - 1; i >= 0; i-- {
		frame := frames[i]
		if frame.Opcode != mostWantedStateOpcode {
			continue
		}
		if len(frame.Payload) != 12 {
			return wantedState{}, false
		}
		return wantedState{
			level: int64(frame.Payload[0]), into: int64(frame.Payload[1]),
			job: int64(frame.Payload[2]), per: int64(frame.Payload[3]),
			lapse: int64(binary.LittleEndian.Uint64(frame.Payload[4:])),
		}, true
	}
	return wantedState{}, false
}

/*
================
TestMostWantedStateFollowsACountedKill

A counted kill carries the holder's new wanted state: progress into
the level below the cap, a full bar at it, and the lapse a raised level
just stamped. Off, no state frame is sent and nothing moves.
================
*/
func TestMostWantedStateFollowsACountedKill(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, killer, victim := newPvpPair(t)
		rt.MostWanted = on
		killer.MostWanted = &domain.MostWanted{Streak: 4}
		kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
		actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1000)
		state, sent := decodeWantedState(actor)
		if !on {
			if sent || killer.MostWanted.Streak != 4 || killer.MostWanted.ExpiresAtMs != 0 {
				t.Fatalf("off: state %+v sent %v, streak %+v", state, sent, killer.MostWanted)
			}
			continue
		}
		if !sent || state.level != 1 || state.into != 0 || state.per != mostWantedKillsPerLevel ||
			state.lapse != 1000+mostWantedLapseMs {
			t.Fatalf("on: state %+v sent %v", state, sent)
		}
		if killer.MostWanted.Streak != 5 || killer.MostWanted.ExpiresAtMs != 1000+mostWantedLapseMs {
			t.Fatalf("on: streak %+v", killer.MostWanted)
		}
	}
}

/*
================
TestMostWantedStateProgressAndCapRenewal

Below a raise nothing stamps: the state shows the kills into the next
level and no lapse. At the cap a counted kill renews the lapse, and
the state shows a full bar (into == kills per level).
================
*/
func TestMostWantedStateProgressAndCapRenewal(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted = true
	killer.MostWanted = &domain.MostWanted{Streak: 2}
	kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
	actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1000)
	state, sent := decodeWantedState(actor)
	if !sent || state.level != 0 || state.into != 3 || state.lapse != 0 {
		t.Fatalf("progress: state %+v sent %v", state, sent)
	}
	if killer.MostWanted.ExpiresAtMs != 0 {
		t.Fatalf("no raise, yet a lapse stamped: %+v", killer.MostWanted)
	}

	rt, _, killer, victim = newPvpPair(t)
	rt.MostWanted = true
	killer.MostWanted = &domain.MostWanted{Streak: 50, ExpiresAtMs: 5000}
	kill = playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
	actor, _, _ = rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 2000)
	state, sent = decodeWantedState(actor)
	if killer.MostWanted.ExpiresAtMs != 2000+mostWantedLapseMs {
		t.Fatalf("cap: lapse %+v, want the renewal", killer.MostWanted)
	}
	if !sent || state.level != mostWantedMaxLevel || state.into != state.per || state.lapse != 2000+mostWantedLapseMs {
		t.Fatalf("cap: state %+v sent %v", state, sent)
	}
}

/*
================
TestMostWantedLapsedKillerRestarts

A killer whose level lapsed starts over from the next counted kill:
the cleared state rides first, then the fresh streak's, and no lapse
is stamped until a level rises again.
================
*/
func TestMostWantedLapsedKillerRestarts(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted = true
	killer.MostWanted = &domain.MostWanted{Streak: 25, ExpiresAtMs: 500}
	kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
	actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1000)
	states := 0
	for _, frame := range actor {
		if frame.Opcode == mostWantedStateOpcode {
			states++
		}
	}
	state, sent := decodeWantedState(actor)
	if killer.MostWanted == nil || killer.MostWanted.Streak != 1 || killer.MostWanted.ExpiresAtMs != 0 {
		t.Fatalf("restarted streak %+v, want one fresh kill", killer.MostWanted)
	}
	if states < 2 || !sent || state.level != 0 || state.into != 1 || state.lapse != 0 {
		t.Fatalf("states %d, last %+v sent %v", states, state, sent)
	}
}

/*
================
TestMostWantedLapsedVictimPaysNothing

A victim whose level lapsed before the kill neither pays a minted
bounty nor owes a transferred one; the same victim before the lapse
pays in full. The lapse is judged from the withVictim snapshot, the
record being already cleared by the death.
================
*/
func TestMostWantedLapsedVictimPaysNothing(t *testing.T) {
	for _, transfer := range []bool{false, true} {
		for _, lapse := range []int64{500, 2000} {
			rt, _, killer, victim := newPvpPair(t)
			rt.MostWanted, rt.MostWantedBountyTransfer = true, transfer
			victim.MostWanted = &domain.MostWanted{Streak: 10, ExpiresAtMs: lapse}
			gold := goldOf(killer)
			kill := playerKill{kind: pk.DeathPlayer, mostWantedStrangers: true}.withVictim(killer, victim)
			_, _, after := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1000)
			if lapse < 1000 {
				if goldOf(killer) != gold || len(after.bounties) != 0 {
					t.Fatalf("transfer %v lapsed: gold %d -> %d, owed %+v", transfer, gold, goldOf(killer), after.bounties)
				}
				continue
			}
			if transfer && (len(after.bounties) != 1 || after.bounties[0].gold != 2*mostWantedBountyPerLevel) {
				t.Fatalf("transfer: owed %+v", after.bounties)
			}
			if !transfer && goldOf(killer)-gold != 2*mostWantedBountyPerLevel {
				t.Fatalf("minted: gold %d -> %d", gold, goldOf(killer))
			}
		}
	}
}

/*
================
TestMostWantedStripKeepsTheStreak

Taking the suit off no longer clears anything: the streak waits, its
bonuses shelve (mostWantedWrites gates on the dressed job) and the
state rides with the suit's job gone. Wearing it again restores both.
================
*/
func TestMostWantedStripKeepsTheStreak(t *testing.T) {
	const town = uint16(0x61A8)
	rt, clock, c := jobFixture(t)
	rt.MostWanted = true
	c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Merchant"}
	c.MostWanted = &domain.MostWanted{Streak: 10}
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == int64(jobTestSuitSlot) {
			c.MissionInventory[i].Slot = int64(jobSuitSlot)
		}
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(world *simulation.WorldState) {
		world.Spawn = simulation.Spawn{RegionID: town, X: 960, Y: 20, Z: 960}
	})
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	if rt.mostWantedWrites(c) == nil {
		t.Fatal("a suited level-2 holder has no writes")
	}

	strip := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobSuitSlot, DestSlot: jobTestSuitSlot, Quantity: 1})
	rt.HandleItemMove(testDivision, c, strip)
	clock.now = clock.now.Add(jobDressSeconds * time.Second)
	pushed = nil
	rt.advanceJobDresses(clock.NowMs())
	state, sent := decodeWantedState(pushed)
	if c.MostWanted == nil || c.MostWanted.Streak != 10 || rt.mostWantedWrites(c) != nil {
		t.Fatalf("after the strip: streak %+v, writes %v", c.MostWanted, rt.mostWantedWrites(c))
	}
	if !sent || state.level != 2 || state.job != 0 {
		t.Fatalf("after the strip: state %+v sent %v, want level 2 unsuited", state, sent)
	}

	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobTestSuitSlot, DestSlot: jobSuitSlot, Quantity: 1})
	rt.HandleItemMove(testDivision, c, wear)
	clock.now = clock.now.Add(jobDressSeconds * time.Second)
	pushed = nil
	rt.advanceJobDresses(clock.NowMs())
	state, sent = decodeWantedState(pushed)
	if c.MostWanted == nil || c.MostWanted.Streak != 10 || rt.mostWantedWrites(c) == nil {
		t.Fatalf("after the re-wear: streak %+v, writes %v", c.MostWanted, rt.mostWantedWrites(c))
	}
	if !sent || state.level != 2 || state.job != int64(domain.JobTrader) {
		t.Fatalf("after the re-wear: state %+v sent %v, want level 2 trader", state, sent)
	}
}

/*
================
TestMostWantedSweepSendsThenClears

World entry tracks a holder for the sweep: nothing is sent until it is
tracked, the entry state goes out once, and when the lapse arrives the
sweep clears the streak and drops the row with a level-0 state.
Tracking with the rule off tracks nothing.
================
*/
func TestMostWantedSweepSendsThenClears(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	lapse := clock.NowMs() + 1000
	character.MostWanted = &domain.MostWanted{Streak: 10, ExpiresAtMs: lapse}
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }

	rt.MostWanted = false
	rt.TrackMostWanted(testDivision, character.Name)
	rt.advanceMostWanted(clock.NowMs())
	rt.MostWanted = true
	rt.advanceMostWanted(clock.NowMs())
	if len(pushed) != 0 {
		t.Fatalf("an untracked or off holder was sent %+v", pushed)
	}

	rt.TrackMostWanted(testDivision, character.Name)
	rt.advanceMostWanted(clock.NowMs())
	state, sent := decodeWantedState(pushed)
	if !sent || state.level != 2 || state.lapse != lapse {
		t.Fatalf("entry state %+v sent %v", state, sent)
	}
	rt.advanceMostWanted(clock.NowMs())
	if state, resent := decodeWantedState(pushed[1:]); resent {
		t.Fatalf("the entry state was sent twice: %+v", state)
	}

	rt.advanceMostWanted(lapse)
	state, sent = decodeWantedState(pushed)
	if character.MostWanted != nil {
		t.Fatalf("the lapsed streak survived: %+v", character.MostWanted)
	}
	if !sent || state.level != 0 || state.lapse != 0 {
		t.Fatalf("the lapse state %+v sent %v, want the cleared row", state, sent)
	}
}
