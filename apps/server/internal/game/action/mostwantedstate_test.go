/*
===========================================================================

mostwantedstate_test.go - the wanted state frame, the decay, the suit

Port-only, not native (#1849, #2721). The buff-board state (0x3FBA)
follows a counted kill, every counted kill restamps the next decay
step one day out, each day without one drops the level by one (down
to none), a decayed streak counts on its lowered level and pays its
decayed bounty as a victim, taking the suit off keeps the streak
(only its bonuses shelve) and the tick sweep sends a tracked holder
its entry state, then judges its decay steps as they land. With
SRO_MOST_WANTED off, no state frame is ever sent.

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
	level  int64
	into   int64
	job    int64
	per    int64
	bounty int64
	decay  int64
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
		if len(frame.Payload) != 20 {
			return wantedState{}, false
		}
		return wantedState{
			level: int64(frame.Payload[0]), into: int64(frame.Payload[1]),
			job: int64(frame.Payload[2]), per: int64(frame.Payload[3]),
			bounty: int64(binary.LittleEndian.Uint64(frame.Payload[4:])),
			decay:  int64(binary.LittleEndian.Uint64(frame.Payload[12:])),
		}, true
	}
	return wantedState{}, false
}

/*
================
TestMostWantedStateFollowsACountedKill

A counted kill carries the holder's new wanted state: progress into
the level below the cap, a full bar at it, the level's bounty and a
full day until the next decay step. Off, no state frame is sent and
nothing moves.
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
			if sent || killer.MostWanted.Streak != 4 || killer.MostWanted.NextDecayAtMs != 0 {
				t.Fatalf("off: state %+v sent %v, streak %+v", state, sent, killer.MostWanted)
			}
			continue
		}
		if !sent || state.level != 1 || state.into != 0 || state.per != mostWantedKillsPerLevel ||
			state.bounty != mostWantedBountyPerLevel || state.decay != mostWantedDecayMs {
			t.Fatalf("on: state %+v sent %v", state, sent)
		}
		if killer.MostWanted.Streak != 5 || killer.MostWanted.NextDecayAtMs != 1000+mostWantedDecayMs {
			t.Fatalf("on: streak %+v", killer.MostWanted)
		}
	}
}

/*
================
TestMostWantedStateRestampsEveryCountedKill

Below a raise nothing used to stamp; now every counted kill restamps
the decay (#2721), so the state always shows a full day. At the cap a
counted kill restamps too, and the state shows a full bar
(into == kills per level) and the cap's bounty.
================
*/
func TestMostWantedStateRestampsEveryCountedKill(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted = true
	killer.MostWanted = &domain.MostWanted{Streak: 2}
	kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
	actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1000)
	state, sent := decodeWantedState(actor)
	if !sent || state.level != 0 || state.into != 3 || state.decay != mostWantedDecayMs {
		t.Fatalf("progress: state %+v sent %v", state, sent)
	}
	if killer.MostWanted.NextDecayAtMs != 1000+mostWantedDecayMs {
		t.Fatalf("no raise, yet no decay stamp: %+v", killer.MostWanted)
	}

	rt, _, killer, victim = newPvpPair(t)
	rt.MostWanted = true
	killer.MostWanted = &domain.MostWanted{Streak: 50, NextDecayAtMs: 5000}
	kill = playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
	actor, _, _ = rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 2000)
	state, sent = decodeWantedState(actor)
	if killer.MostWanted.NextDecayAtMs != 2000+mostWantedDecayMs {
		t.Fatalf("cap: stamp %+v, want the restamp", killer.MostWanted)
	}
	if !sent || state.level != mostWantedMaxLevel || state.into != state.per ||
		state.bounty != mostWantedMaxLevel*mostWantedBountyPerLevel || state.decay != mostWantedDecayMs {
		t.Fatalf("cap: state %+v sent %v", state, sent)
	}
}

/*
================
TestMostWantedDecayedKillerCountsOnTheLoweredStreak

A killer whose decay steps came due counts on the lowered streak:
two days down from level 5 the kill lands on level 3's streak, and
the state the decay sent already showed the lowered level.
================
*/
func TestMostWantedDecayedKillerCountsOnTheLoweredStreak(t *testing.T) {
	now := int64(3 * mostWantedDecayMs)
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted = true
	killer.MostWanted = &domain.MostWanted{Streak: 25, NextDecayAtMs: now - mostWantedDecayMs}
	kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
	actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, now)
	state, sent := decodeWantedState(actor)
	if killer.MostWanted.Streak != 16 || killer.MostWanted.NextDecayAtMs != now+mostWantedDecayMs {
		t.Fatalf("decayed streak %+v, want 16 counted on the lowered 15", killer.MostWanted)
	}
	if !sent || state.level != 3 || state.bounty != 3*mostWantedBountyPerLevel || state.decay != mostWantedDecayMs {
		t.Fatalf("state %+v sent %v", state, sent)
	}
}

/*
================
TestMostWantedDecayedKillerRestarts

A killer the decay took to none starts over from the next counted
kill: the cleared state rides first, then the fresh streak's, stamped
a day out like any counted kill's.
================
*/
func TestMostWantedDecayedKillerRestarts(t *testing.T) {
	now := int64(3 * mostWantedDecayMs)
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted = true
	killer.MostWanted = &domain.MostWanted{Streak: 10, NextDecayAtMs: now - 2*mostWantedDecayMs}
	kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
	actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, now)
	states := 0
	for _, frame := range actor {
		if frame.Opcode == mostWantedStateOpcode {
			states++
		}
	}
	state, sent := decodeWantedState(actor)
	if killer.MostWanted == nil || killer.MostWanted.Streak != 1 ||
		killer.MostWanted.NextDecayAtMs != now+mostWantedDecayMs {
		t.Fatalf("restarted streak %+v, want one fresh kill", killer.MostWanted)
	}
	if states < 2 || !sent || state.level != 0 || state.into != 1 || state.decay != mostWantedDecayMs {
		t.Fatalf("states %d, last %+v sent %v", states, state, sent)
	}
}

/*
================
TestMostWantedVictimPaysTheDecayedLevel

A victim pays the bounty of the level it still holds at the kill:
unstamped and not-yet-due streaks pay in full, one due step pays one
level less, and a streak the decay took to none pays nothing, minted
or transferred. The decay is judged from the withVictim snapshot, the
record being already cleared by the death.
================
*/
func TestMostWantedVictimPaysTheDecayedLevel(t *testing.T) {
	now := int64(3 * mostWantedDecayMs)
	for _, transfer := range []bool{false, true} {
		for _, tc := range []struct {
			stamp int64
			want  int64
		}{
			{0, 2},
			{now + mostWantedDecayMs, 2},
			{now - 1, 1},
			{now - mostWantedDecayMs, 0},
		} {
			rt, _, killer, victim := newPvpPair(t)
			rt.MostWanted, rt.MostWantedBountyTransfer = true, transfer
			victim.MostWanted = &domain.MostWanted{Streak: 10, NextDecayAtMs: tc.stamp}
			gold := goldOf(killer)
			kill := playerKill{kind: pk.DeathPlayer, mostWantedStrangers: true}.withVictim(killer, victim)
			_, _, after := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, now)
			if transfer {
				if tc.want == 0 && len(after.bounties) != 0 {
					t.Fatalf("transfer stamp %d: owed %+v, want none", tc.stamp, after.bounties)
				}
				if tc.want > 0 && (len(after.bounties) != 1 || after.bounties[0].gold != uint64(tc.want)*mostWantedBountyPerLevel) {
					t.Fatalf("transfer stamp %d: owed %+v", tc.stamp, after.bounties)
				}
				continue
			}
			if got := goldOf(killer) - gold; got != uint64(tc.want)*mostWantedBountyPerLevel {
				t.Fatalf("minted stamp %d: gold +%d, want %d levels", tc.stamp, got, tc.want)
			}
		}
	}
}

/*
================
TestMostWantedDecayedLevel

The read-only decay judgement a victim's bounty reads: an unstamped
streak never decays, a step lands on its day, and the surplus past
the cap feeds the first steps.
================
*/
func TestMostWantedDecayedLevel(t *testing.T) {
	day := int64(mostWantedDecayMs)
	for _, tc := range []struct {
		streak, stamp, now, want int64
	}{
		{10, 0, 100 * day, 2},
		{10, 5 * day, 5*day - 1, 2},
		{10, 5 * day, 5 * day, 1},
		{15, 5 * day, 6 * day, 1},
		{55, 5 * day, 5 * day, 10},
		{50, 5 * day, 15 * day, 0},
	} {
		if got := mostWantedDecayedLevel(tc.streak, tc.stamp, tc.now); got != tc.want {
			t.Fatalf("level(%d, stamp %d, now %d) = %d, want %d", tc.streak, tc.stamp, tc.now, got, tc.want)
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
TestMostWantedSweepSendsThenDecays

World entry tracks a holder for the sweep: nothing is sent until it
is tracked, the entry state goes out once with the time left to the
first step, each step drops one level (and restarts the countdown
full), and the last one clears the streak and drops the row.
Tracking with the rule off tracks nothing.
================
*/
func TestMostWantedSweepSendsThenDecays(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	stamp := clock.NowMs() + 1000
	character.MostWanted = &domain.MostWanted{Streak: 10, NextDecayAtMs: stamp}
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
	if !sent || state.level != 2 || state.bounty != 2*mostWantedBountyPerLevel || state.decay != 1000 {
		t.Fatalf("entry state %+v sent %v", state, sent)
	}
	rt.advanceMostWanted(clock.NowMs())
	if state, resent := decodeWantedState(pushed[1:]); resent {
		t.Fatalf("the entry state was sent twice: %+v", state)
	}

	rt.advanceMostWanted(stamp)
	if character.MostWanted == nil || character.MostWanted.Streak != 5 ||
		character.MostWanted.NextDecayAtMs != stamp+mostWantedDecayMs {
		t.Fatalf("after one step: %+v", character.MostWanted)
	}
	state, sent = decodeWantedState(pushed)
	if !sent || state.level != 1 || state.bounty != mostWantedBountyPerLevel || state.decay != mostWantedDecayMs {
		t.Fatalf("the step state %+v sent %v, want level 1 with a full day", state, sent)
	}

	rt.advanceMostWanted(stamp + mostWantedDecayMs)
	if character.MostWanted != nil {
		t.Fatalf("the decayed-to-none streak survived: %+v", character.MostWanted)
	}
	state, sent = decodeWantedState(pushed)
	if !sent || state.level != 0 || state.decay != 0 || state.bounty != 0 {
		t.Fatalf("the cleared state %+v sent %v, want the dropped row", state, sent)
	}
}

/*
================
TestMostWantedDecayDropsOneLevelPerDay

The done-when boundaries (#2721): 23h59m without a counted kill drops
nothing, 24h drops exactly one level, 48h drops two - judged in one
late pass - and a late judgement keeps the cadence (the stamp
advances by the steps taken, never to now).
================
*/
func TestMostWantedDecayDropsOneLevelPerDay(t *testing.T) {
	stamp := int64(10 * mostWantedDecayMs)
	record := &domain.MostWanted{Streak: 15, NextDecayAtMs: stamp}
	for _, tc := range []struct {
		now, drops int64
	}{
		{stamp - 60_000, 0},
		{stamp, 1},
		{stamp + mostWantedDecayMs, 2},
		{stamp + 36*60*60_000, 2},
	} {
		if got := mostWantedDecayDrops(record, tc.now); got != tc.drops {
			t.Fatalf("drops at %d = %d, want %d", tc.now, got, tc.drops)
		}
	}

	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	now := clock.NowMs()
	character.MostWanted = &domain.MostWanted{Streak: 15, NextDecayAtMs: now + mostWantedDecayMs}
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	rt.MostWanted = true
	rt.TrackMostWanted(testDivision, character.Name)

	// 23h59m after the counted kill: not due, nothing drops, nothing sent.
	rt.advanceMostWanted(now + mostWantedDecayMs - 60_000)
	if character.MostWanted.Streak != 15 {
		t.Fatalf("23h59m dropped the streak to %+v", character.MostWanted)
	}
	pushed = nil

	// 48h after it, judged in one pass: two levels drop and the stamp
	// advances by both steps (not to the late now).
	rt.advanceMostWanted(now + 2*mostWantedDecayMs)
	if character.MostWanted.Streak != 5 ||
		character.MostWanted.NextDecayAtMs != now+3*mostWantedDecayMs {
		t.Fatalf("48h in one pass: %+v, want streak 5 on the kept cadence", character.MostWanted)
	}
	state, sent := decodeWantedState(pushed)
	if !sent || state.level != 1 || state.decay != mostWantedDecayMs {
		t.Fatalf("the 48h state %+v sent %v", state, sent)
	}
}
