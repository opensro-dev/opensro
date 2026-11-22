/*
===========================================================================

petrecovery_test.go - tests for petrecovery.go: no stare, the owner told why

An owner's order the pet cannot carry out ends at once and answers the
owner with the client's obstacle refusal; a fight the pet picked itself
ends without one; a brood relay never sends a yielded child straight
back; the follow watchdog judges only pets that are away from home and
not closing; every resolution is counted by kind and region.

===========================================================================
*/

package action

import (
	"bytes"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wallInFrontOfTarget

Puts the fixture pet 60 units from its target with every step refused
where it stands and no route known: the target cannot be reached.
================
*/
func wallInFrontOfTarget(t *testing.T, band uint16) (*Runtime, *fakeClock, petOwnerKey, *petSession, uint32) {
	t.Helper()
	rt, clock, c, m := newPetCombatRuntime(t, 1_000_000, band)
	petGID := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.mover.AnyFollower() == nil {
		t.Fatal("fixture pet has no follower")
	}
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	state.mover.FollowFrom(petGID, simulation.Spawn{RegionID: live.Spawn.RegionID, X: live.Spawn.X - 60, Y: live.Spawn.Y, Z: live.Spawn.Z})
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return from, nil }
	return rt, clock, petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: petGID}, state, m.Gid
}

/*
================
obstacleRefusals

The 0xB69E attack refusals for an obstacle in frames.
================
*/
func obstacleRefusals(frames []simulation.Frame, cosGID uint32) int {
	want := wire.NewWriter(7).U8(2).U8(wire.CosCommandAttackTag).U8(petObstacleRefusal).U32(cosGID).Payload()
	n := 0
	for _, frame := range frames {
		if frame.Opcode == wire.OpCosCommandResult && bytes.Equal(frame.Payload, want) {
			n++
		}
	}
	return n
}

/*
================
TestUnreachableOrderEndsAtOnceAndTellsTheOwner
================
*/
func TestUnreachableOrderEndsAtOnceAndTellsTheOwner(t *testing.T) {
	rt, clock, key, state, target := wallInFrontOfTarget(t, attackPetBand)
	rt.HandleCosCommand(testDivision, state.character, petAttackOrder(key.gid, target))
	if state.combat == nil || !state.combat.ordered {
		t.Fatal("the owner's order left no ordered fight")
	}
	var frames []simulation.Frame
	endedAt := -1
	for i := 1; i <= 50; i++ {
		frames = append(frames, rt.advancePet(key, clock.NowMs()+int64(i)*100)...)
		if state.combat == nil && endedAt < 0 {
			endedAt = i
		}
	}
	if endedAt < 0 || endedAt > 2 {
		t.Fatalf("the unreachable order ended on tick %d, want at once", endedAt)
	}
	if n := obstacleRefusals(frames, key.gid); n != 1 {
		t.Fatalf("the owner got %d obstacle refusals, want exactly one", n)
	}
	if state.stallTarget != target {
		t.Fatal("the yielded target was not set aside from sight acquisition")
	}
	if got := rt.PetCombatCounters()["recovery.battle_no_route"]; got != 1 {
		t.Fatalf("recovery.battle_no_route = %d, want 1", got)
	}
}

/*
================
TestSelfPickedFightEndsWithoutANotice

A soldier that acquired its target by sight was given no order: its yield
is silent.
================
*/
func TestSelfPickedFightEndsWithoutANotice(t *testing.T) {
	rt, clock, key, state, _ := wallInFrontOfTarget(t, domain.MercenaryBand)
	var frames []simulation.Frame
	acquired, ended := false, false
	for i := 1; i <= 100 && !ended; i++ {
		frames = append(frames, rt.advancePet(key, clock.NowMs()+int64(i)*100)...)
		acquired = acquired || state.combat != nil
		ended = acquired && state.combat == nil
	}
	if !ended {
		t.Fatal("the soldier never acquired and yielded its unreachable target")
	}
	if n := obstacleRefusals(frames, key.gid); n != 0 {
		t.Fatalf("a self-picked fight sent %d obstacle refusals, want none", n)
	}
}

/*
================
TestBroodRelaySitsOutTheYieldButTheOwnerDoesNot
================
*/
func TestBroodRelaySitsOutTheYieldButTheOwnerDoesNot(t *testing.T) {
	rt, clock, key, state, target := wallInFrontOfTarget(t, attackPetBand)
	now := clock.NowMs()
	state.stallTarget, state.stallUntilMs = target, now+petStallCooldownMs
	rt.relayPetAttack(testDivision, state.character, key.gid, target, now, rt.tickDoors())
	if state.combat != nil {
		t.Fatal("a brood relay sent the child straight back to the target it yielded on")
	}
	rt.relayPetAttack(testDivision, state.character, key.gid, target, now+petStallCooldownMs, rt.tickDoors())
	if state.combat == nil || state.combat.ordered {
		t.Fatalf("a relay after the cooldown left %+v, want an un-ordered fight", state.combat)
	}
	rt.cancelPetCombat(key, state, now)
	state.stallTarget, state.stallUntilMs = target, now+petStallCooldownMs
	rt.orderPetAttack(testDivision, state.character, state.character, key.gid, target, now, rt.tickDoors())
	if state.combat == nil || !state.combat.ordered || state.stallTarget != 0 {
		t.Fatal("the owner's order inside the cooldown was not obeyed as an ordered fight")
	}
}

/*
================
TestOwnerTakingUpTheSameFightOrdersIt
================
*/
func TestOwnerTakingUpTheSameFightOrdersIt(t *testing.T) {
	rt, clock, key, state, target := wallInFrontOfTarget(t, attackPetBand)
	rt.relayPetAttack(testDivision, state.character, key.gid, target, clock.NowMs(), rt.tickDoors())
	if state.combat == nil || state.combat.ordered {
		t.Fatal("fixture: the relay left no un-ordered fight")
	}
	rt.orderPetAttack(testDivision, state.character, state.character, key.gid, target, clock.NowMs(), rt.tickDoors())
	if !state.combat.ordered {
		t.Fatal("the owner ordering the fight the pet already had did not make it theirs")
	}
}

/*
================
TestFollowWatchdog

The judge alone (petFollowStuck), with no blocked verdict in play: a pet
beyond the follow distance that neither closes nor moves is stuck after
the window; one that keeps moving, one at home, one on another plane and a
transport never are.
================
*/
func TestFollowWatchdog(t *testing.T) {
	owner := simulation.Spawn{RegionID: 0x62a8, X: 100, Z: 500}
	away := simulation.Spawn{RegionID: 0x62a8, X: 300, Z: 500}
	attackTid := uint16(0x1c6 | attackPetBand<<11)
	judge := func(state *petSession, tid uint16, at simulation.Spawn, moveBy float64) bool {
		f := state.mover.AnyFollower()
		f.Displace(at, 0)
		for now := int64(0); now <= petProgressStallMs+100; now += 100 {
			pose := f.Position(now)
			if moveBy != 0 {
				pose.X += moveBy
				f.Displace(pose, now)
			}
			if _, stuck := (&Runtime{}).petFollowStuck(state, tid, owner, now); stuck {
				return true
			}
		}
		return false
	}
	fresh := func(at simulation.Spawn) *petSession {
		state := &petSession{}
		state.mover.FollowFrom(77, at)
		return state
	}
	if !judge(fresh(away), attackTid, away, 0) {
		t.Fatal("a pet standing 200 units from its owner for the window was not judged stuck")
	}
	if judge(fresh(away), attackTid, away, 5) {
		t.Fatal("a pet that keeps moving was judged stuck")
	}
	home := simulation.Spawn{RegionID: 0x62a8, X: 140, Z: 500}
	if judge(fresh(home), attackTid, home, 0) {
		t.Fatal("a pet within the follow distance was judged stuck")
	}
	below := simulation.Spawn{RegionID: 0x8001, X: 300, Z: 500}
	if judge(fresh(below), attackTid, below, 0) {
		t.Fatal("a pet on another plane was judged stuck; that is the owner-follow warp's case")
	}
	if judge(fresh(away), uint16(0x1c6|2<<11), away, 0) {
		t.Fatal("a transport was judged here; its own rules answer it")
	}
	// A blocked verdict left by a move toward something else (an item the
	// pickup gave up on) is not a follow verdict.
	state := fresh(away)
	refuse := func(from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return from, &simulation.MoveError{Reason: "walled in"}
	}
	state.mover.AnyFollower().Approach(simulation.Spawn{RegionID: 0x62a8, X: 900, Z: 500}, 100, 100, 1, refuse)
	if !state.mover.AnyFollower().Reach().Blocked {
		t.Fatal("fixture: the item approach was not blocked")
	}
	if _, stuck := (&Runtime{}).petFollowStuck(state, attackTid, owner, 100); stuck {
		t.Fatal("a pickup's blocked verdict relocated a pet as if its follow had no way")
	}
	state.mover.AnyFollower().Approach(owner, 100, 200, simulation.PetFollowDistance, refuse)
	if kind, stuck := (&Runtime{}).petFollowStuck(state, attackTid, owner, 200); !stuck || kind != petRecoveryFollowNoRoute {
		t.Fatal("a follow move with no way home was not judged at once")
	}
}

/*
================
TestRecoveryCountersByRegion
================
*/
func TestRecoveryCountersByRegion(t *testing.T) {
	var c petRecoveryCounters
	for region := 0; region <= petRecoveryMaxRegions; region++ {
		c.count(petRecoveryFollowStall, uint16(region))
	}
	c.count(petRecoveryFollowStall, 7)
	c.count(petRecoveryFollowStall, uint16(petRecoveryMaxRegions+1))
	out := map[string]uint64{}
	c.into(out)
	if out["recovery.follow_stall"] != petRecoveryMaxRegions+3 {
		t.Fatalf("total %d, want every count", out["recovery.follow_stall"])
	}
	if out["recovery.follow_stall.region.7"] != 2 {
		t.Fatalf("region 7 = %d, want 2", out["recovery.follow_stall.region.7"])
	}
	if out["recovery.follow_stall.region.other"] != 2 {
		t.Fatalf("other = %d, want the two counts past the region bound", out["recovery.follow_stall.region.other"])
	}
	if _, ok := out["recovery.battle_stall"]; !ok {
		t.Fatal("a kind never counted is missing from the block; want it at zero")
	}
}
