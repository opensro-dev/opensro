/*
===========================================================================

petstall_test.go - a battle approach that cannot reach gives up to FOLLOW

Port-only recovery (petrecovery.go): native BATTLE's stuck answer retains
the fight, so a pet ordered onto a target past a wall could stand there
for as long as the target lived. An approach with no way to strike range
yields on its first judged tick; one whose way is known but never closes
yields after petProgressStallMs. Either way the pet leaves BATTLE, returns
to FOLLOW, and is brought home. A pursuit that keeps closing is never
"stalled", however long it runs.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus/hooks/test"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestWedgedBattleApproachYieldsToFollow

The live pet-stuck report: every approach segment of an ordered wolf
stops where it starts (the wall it cannot round) and no route leads past.
The pet must leave BATTLE at once instead of standing at the wall until a
return scroll, and the follow lane brings it beside its owner.
================
*/
func TestWedgedBattleApproachYieldsToFollow(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()
	rt, clock, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	petGID := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.mover.AnyFollower() == nil {
		t.Fatal("fixture pet has no follower")
	}
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	stall := simulation.Spawn{RegionID: live.Spawn.RegionID, X: live.Spawn.X - 60, Y: live.Spawn.Y, Z: live.Spawn.Z}
	state.mover.FollowFrom(petGID, stall)
	// Every segment stops where it starts: no approach leg can close.
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return from, nil }
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	if state.combat == nil {
		t.Fatal("the attack order left no combat intent")
	}

	tick := func() {
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	clearedAt := -1
	for step := 1; step <= 100; step++ {
		tick()
		if state.combat == nil {
			clearedAt = step
			break
		}
	}
	if clearedAt < 0 {
		t.Fatal("the wedged battle was never yielded: the pet still fights after 10 s (the live freeze)")
	}
	if clearedAt > 3 {
		t.Fatalf("the battle with no way yielded after %d ticks, want at once (its second pursuit tick)", clearedAt)
	}
	if pet := c.CompanionByGID(petGID); pet == nil || pet.CurrentHP == 0 {
		t.Fatal("the yielded pet is dead; the live reports say alive")
	}
	stalled := 0
	for _, entry := range hook.AllEntries() {
		if entry.Data["reason"] == "approach not reaching strike range" {
			stalled++
		}
	}
	if stalled != 1 {
		t.Fatalf("stall reported %d times, want the one report the live server logged", stalled)
	}

	// On FOLLOW the pet is brought beside its owner.
	for range 100 {
		tick()
	}
	if state.combat != nil {
		t.Fatal("the yielded pet re-entered battle on its own")
	}
	owner := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	pet := state.mover.AnyFollower().Position(clock.NowMs())
	if distance := simulation.WorldDistance2D(pet, owner); distance > 61 {
		t.Fatalf("the yielded pet ended %.1f units from its owner (stall spot %+v, owner %+v): want the follow stand-off, well inside the %d cancel range",
			distance, stall, owner, cosCancelRange)
	}
}

/*
================
TestRoutedButNeverClosingBattleYieldsAtTheWatchdog

The watchdog: a route to the target is known, so no move is ever judged
"no way", but every step is refused. The fight holds through the native
stuck answer (BATTLE retains it) and the watchdog ends it at the stall
window, counted as battle_stall.
================
*/
func TestRoutedButNeverClosingBattleYieldsAtTheWatchdog(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	petGID := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	state.mover.FollowFrom(petGID, simulation.Spawn{RegionID: live.Spawn.RegionID, X: live.Spawn.X - 60, Y: live.Spawn.Y, Z: live.Spawn.Z})
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return from, nil }
	rt.PlanCompanionRoute = func(from, goal simulation.Spawn) []simulation.Spawn {
		return []simulation.Spawn{from, {RegionID: from.RegionID, X: from.X, Z: from.Z + 100}, goal}
	}
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	clearedAt := -1
	for step := 1; step <= 100 && clearedAt < 0; step++ {
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
		if state.combat == nil {
			clearedAt = step
		}
	}
	if clearedAt < 25 || clearedAt > 45 {
		t.Fatalf("the routed but never closing battle yielded after %d ticks, want the %d ms watchdog at the 100 ms cadence", clearedAt, petProgressStallMs)
	}
	counters := rt.PetCombatCounters()
	if counters["recovery.battle_stall"] != 1 || counters["recovery.battle_no_route"] != 0 {
		t.Fatalf("counted %v, want one battle_stall", counters)
	}
}

/*
================
TestOwnerOrderInsideCooldownIsObeyed

Agency over cooldown: an admitted owner order on the yielded target
clears that target's cooldown and stands - the pet obeys its owner,
and a pursuit that wedges again simply yields again. The cooldown
binds only the un-ordered re-picks (the mercenary sight acquisition,
next test), never the owner's word.
================
*/
func TestOwnerOrderInsideCooldownIsObeyed(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	petGID := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.mover.AnyFollower() == nil {
		t.Fatal("fixture pet has no follower")
	}
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	state.mover.FollowFrom(petGID, simulation.Spawn{RegionID: live.Spawn.RegionID, X: live.Spawn.X - 60, Y: live.Spawn.Y, Z: live.Spawn.Z})
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return from, nil }
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	if state.combat == nil {
		t.Fatal("the attack order left no combat intent")
	}
	tick := func(n int) {
		for range n {
			clock.now = clock.now.Add(100 * time.Millisecond)
			rt.TickHook()(clock.NowMs())
		}
	}
	for step := 0; step < 100 && state.combat != nil; step++ {
		tick(1)
	}
	if state.combat != nil {
		t.Fatal("the wedged battle never yielded")
	}
	if state.stallTarget != m.Gid || state.stallUntilMs <= clock.NowMs() {
		t.Fatalf("the yield left no live cooldown: target %d until %d at now %d", state.stallTarget, state.stallUntilMs, clock.NowMs())
	}

	// The repeated order lands inside the cooldown and is obeyed at
	// once; the cooldown is gone, not sat out.
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	if state.combat == nil {
		t.Fatal("an owner order inside the cooldown was not obeyed")
	}
	if state.stallTarget != 0 || state.stallUntilMs != 0 {
		t.Fatalf("the obeyed order left the cooldown standing: target %d until %d", state.stallTarget, state.stallUntilMs)
	}

	// The re-entered pursuit wedges and yields again, and the next
	// order inside the fresh cooldown is obeyed again: agency repeats,
	// the yield is the only brake.
	for step := 0; step < 100 && state.combat != nil; step++ {
		tick(1)
	}
	if state.combat != nil {
		t.Fatal("the re-entered pursuit never yielded")
	}
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	if state.combat == nil {
		t.Fatal("the second owner order inside a cooldown was not obeyed")
	}
}

/*
================
TestStalledTargetSitsOutSightAcquisition

The mercenary loop is faster: no order at all. The soldier acquires
the wedged monster by sight, yields at the stall, and without the
cooldown re-acquires it on the very next tick - before one follow
stop can count. With the cooldown the soldier stays on the follow
lane, warps home, and only re-acquires the monster (still in sight)
once the cooldown has lapsed.
================
*/
func TestStalledTargetSitsOutSightAcquisition(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 1000000, domain.MercenaryBand)
	petGID := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.mover.AnyFollower() == nil {
		t.Fatal("fixture soldier has no follower")
	}
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	state.mover.FollowFrom(petGID, simulation.Spawn{RegionID: live.Spawn.RegionID, X: live.Spawn.X - 60, Y: live.Spawn.Y, Z: live.Spawn.Z})
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return from, nil }
	tick := func(n int) {
		for range n {
			clock.now = clock.now.Add(100 * time.Millisecond)
			rt.TickHook()(clock.NowMs())
		}
	}
	// Acquired by sight alone, then yielded at the stall.
	acquired := false
	for step := 0; step < 50 && !acquired; step++ {
		tick(1)
		acquired = state.combat != nil
	}
	if !acquired {
		t.Fatal("the soldier never acquired the monster in sight")
	}
	yielded := false
	for step := 0; step < 100 && !yielded; step++ {
		tick(1)
		yielded = state.combat == nil
	}
	if !yielded {
		t.Fatal("the wedged soldier never yielded")
	}

	// Inside the cooldown the monster is in sight the whole time and
	// is nevertheless not re-acquired; the soldier gets home meanwhile.
	for step := 0; step < 50; step++ {
		tick(1)
		if state.combat != nil {
			t.Fatalf("the yielded target was re-acquired %d ticks into its cooldown (the loop)", step+1)
		}
	}
	owner := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	pet := state.mover.AnyFollower().Position(clock.NowMs())
	if distance := simulation.WorldDistance2D(pet, owner); distance > 61 {
		t.Fatalf("the yielded soldier ended %.1f units from its owner inside the cooldown; want the follow stand-off", distance)
	}

	// Once the cooldown lapses, the still-visible monster is fair again.
	reacquired := false
	for step := 0; step < 100 && !reacquired; step++ {
		tick(1)
		reacquired = state.combat != nil
	}
	if !reacquired {
		t.Fatal("the yielded target stayed un-acquired after its cooldown lapsed")
	}
}

/*
================
TestProgressingApproachKeepsFighting

The negative: a pursuit on open ground closes the whole way. It runs
past the stall threshold while closing and keeps its battle: progress,
not pursuit time, is what the stall clock measures.
================
*/
func TestProgressingApproachKeepsFighting(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100, attackPetBand)
	petGID := c.ActiveCOS.GID
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.mover.AnyFollower() == nil {
		t.Fatal("fixture pet has no follower")
	}
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	start := simulation.Spawn{RegionID: live.Spawn.RegionID, X: live.Spawn.X - 600, Y: live.Spawn.Y, Z: live.Spawn.Z}
	state.mover.FollowFrom(petGID, start)
	rt.HandleCosCommand(testDivision, c, petAttackOrder(petGID, m.Gid))
	if state.combat == nil {
		t.Fatal("the attack order left no combat intent")
	}
	// Past petProgressStallMs of pursuit, still closing: the battle stands.
	for range 40 {
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	if state.combat == nil {
		t.Fatal("a closing pursuit lost its battle at the stall threshold")
	}
	pose := state.mover.AnyFollower().Position(clock.NowMs())
	if closed := simulation.WorldDistance2D(start, pose); closed < 100 {
		t.Fatalf("the pet barely closed toward its target: %.1f units of the 600 unit approach", closed)
	}
}
