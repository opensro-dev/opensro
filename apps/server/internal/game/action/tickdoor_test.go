/*
===========================================================================

tickdoor_test.go - character commits follow the caller's lane (#2036)

A tick path that takes an immediate door commits a SQLite transaction
per hit on the tick goroutine (live profile 13:56Z: commitTxLocked was
~12% of tick CPU through acceptSkillAreaAt, acceptSkillStagePhaseAt,
grantPickupTo and friends), so the tick lane takes the deferred doors
and joins the tick's one batched commit (store tickbatch.go).

The lane cuts the other way too, and cutting it wrong is worse: a packet
handler that reaches a deferred door mid-tick answers before its row
commits, so a crash or failed flush between the answer and the flush
loses an acknowledged action (a potion consumed, a pickup granted).
Everything a handler answers is committed before the answer.

These guards pin both sides by label: the handler drives must show the
immediate doors and a commit the store counted before the handler
returned; the tick drives must show the deferred doors and nothing on
the immediate seams.

===========================================================================
*/

package action

import (
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
laneWatch

Records every label that arrives through one lane's Update seams while
delegating to the real door, so the scenario runs unchanged and the
guard reports at the end.
================
*/
type laneWatch struct {
	labels []string
}

/*
================
took
================
*/
func (w *laneWatch) took(label string) bool {
	for _, seen := range w.labels {
		if seen == label {
			return true
		}
	}
	return false
}

/*
================
watchAuthorityDoors

Point a door runtime's four seams at the real authority store behind
two watches, one per lane.
================
*/
func watchAuthorityDoors(d *doorRuntime) (immediate, deferred *laneWatch) {
	deps := d.rt.deps.(*enterworld.Deps)
	immediate, deferred = &laneWatch{}, &laneWatch{}
	deps.UpdateCharacter = func(c *enterworld.Character, label string, update func() bool) bool {
		immediate.labels = append(immediate.labels, label)
		return d.authority.UpdateCharacter(c, label, update)
	}
	deps.UpdateCharacters = func(cs []*enterworld.Character, label string, update func() bool) bool {
		immediate.labels = append(immediate.labels, label)
		return d.authority.UpdateCharacters(cs, label, update)
	}
	deps.UpdateCharacterDeferred = func(c *enterworld.Character, label string, update func() bool) bool {
		deferred.labels = append(deferred.labels, label)
		return d.authority.UpdateCharacterDeferred(c, label, update)
	}
	deps.UpdateCharactersDeferred = func(cs []*enterworld.Character, label string, update func() bool) bool {
		deferred.labels = append(deferred.labels, label)
		return d.authority.UpdateCharactersDeferred(cs, label, update)
	}
	return immediate, deferred
}

/*
================
TestHandlerPickupCommitsBeforeTheAnswer

Through the real authority store with a tick batch open (a tick in
flight): a handler's in-range gold grant must commit through the
immediate door before it answers. The store's commit count has moved
when the handler returns, and the batch flush afterwards holds nothing
of the handler's - so no crash or failed flush can lose the grant.
================
*/
func TestHandlerPickupCommitsBeforeTheAnswer(t *testing.T) {
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	immediate, deferred := watchAuthorityDoors(d)

	initial := goldOfT(t, d.character)
	d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGoldDrop, GoldAmount: 1500,
	}))
	heap := groundByRefObjID(d.rt, 62)
	if heap == nil {
		t.Fatal("gold drop setup failed")
	}
	immediate.labels, deferred.labels = nil, nil

	d.authority.BeginTickBatch()
	commits := d.authority.Health().Commits
	grant := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: heap.Gid}.Encode())
	assertOpcodes(t, grant.Frames,
		wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpPointsUpdate, wire.OpObjectDespawn, wire.OpActionState)
	if !immediate.took("pickup-gold") {
		t.Fatalf("handler grant committed through %v, want the immediate pickup-gold door", immediate.labels)
	}
	if len(deferred.labels) != 0 {
		t.Fatalf("handler grant deferred door(s) %v; its answer would precede the commit", deferred.labels)
	}
	if got := d.authority.Health().Commits; got <= commits {
		t.Fatalf("store commits = %d after the handler answered, want more than the %d before it: the grant was not committed before its answer", got, commits)
	}
	stats := d.authority.FlushTickBatch()
	if stats.Err != nil || stats.Deferred != 0 {
		t.Fatalf("tick flush = %+v, want a clean flush holding none of the handler's work", stats)
	}

	r := d.reboot(t)
	if got := goldOfT(t, r.character); got != initial {
		t.Fatalf("gold after reboot = %d, want the granted %d", got, initial)
	}
}

/*
================
TestTickPickupGrantJoinsTheBatch

The same grant on the tick lane: the handler arms the approach (an
immediate commit, before its own answer), then the pending matures on
the tick and the grant joins the batch - no immediate door fires, the
flush reports the deferred work, and a reboot reads the gold.
================
*/
func TestTickPickupGrantJoinsTheBatch(t *testing.T) {
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	immediate, deferred := watchAuthorityDoors(d)

	initial := goldOfT(t, d.character)
	start := simulation.SeedWorldState(d.character).Spawn
	far := d.rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", d.clock.Now()))
	armed := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: far.Gid}.Encode())
	if armed.Pending == nil {
		t.Fatalf("interact = %+v, want an armed approach", armed)
	}
	immediate.labels, deferred.labels = nil, nil

	d.authority.BeginTickBatch()
	d.rt.advancePendingPickups(d.clock.NowMs() + 120_000)
	if len(immediate.labels) != 0 {
		t.Fatalf("tick grant took immediate door(s) %v, want the deferred doors (#2028)", immediate.labels)
	}
	if !deferred.took("pickup-gold") {
		t.Fatalf("tick grant committed through %v, want the deferred pickup-gold door", deferred.labels)
	}
	stats := d.authority.FlushTickBatch()
	if stats.Err != nil || !stats.Flushed || stats.Deferred == 0 {
		t.Fatalf("tick flush = %+v, want a flushed batch covering deferred doors", stats)
	}

	r := d.reboot(t)
	if got := goldOfT(t, r.character); got != initial+1000 {
		t.Fatalf("gold after reboot = %d, want the granted %d", got, initial+1000)
	}
}

/*
================
watchFixtureDoors

Wrap a combat fixture's four seams with two lane watches, delegating to
whatever the fixture installed.
================
*/
func watchFixtureDoors(rt *Runtime) (immediate, deferred *laneWatch) {
	deps := rt.deps.(*enterworld.Deps)
	immediate, deferred = &laneWatch{}, &laneWatch{}
	update, updateMany := deps.UpdateCharacter, deps.UpdateCharacters
	updateDeferred, updateManyDeferred := deps.UpdateCharacterDeferred, deps.UpdateCharactersDeferred
	deps.UpdateCharacter = func(c *enterworld.Character, label string, fn func() bool) bool {
		immediate.labels = append(immediate.labels, label)
		if update != nil {
			return update(c, label, fn)
		}
		return fn()
	}
	deps.UpdateCharacters = func(cs []*enterworld.Character, label string, fn func() bool) bool {
		immediate.labels = append(immediate.labels, label)
		if updateMany != nil {
			return updateMany(cs, label, fn)
		}
		return fn()
	}
	deps.UpdateCharacterDeferred = func(c *enterworld.Character, label string, fn func() bool) bool {
		deferred.labels = append(deferred.labels, label)
		if updateDeferred != nil {
			return updateDeferred(c, label, fn)
		}
		return fn()
	}
	deps.UpdateCharactersDeferred = func(cs []*enterworld.Character, label string, fn func() bool) bool {
		deferred.labels = append(deferred.labels, label)
		if updateManyDeferred != nil {
			return updateManyDeferred(cs, label, fn)
		}
		return fn()
	}
	return immediate, deferred
}

/*
================
TestHandlerSkillEngageCommitsThroughTheImmediateDoors

A basic-attack engage is a packet handler's answer: its stage commit
(player-basic-attack) takes the immediate many-door, never the batch.
================
*/
func TestHandlerSkillEngageCommitsThroughTheImmediateDoors(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	installStoreDoor(t, rt)
	immediate, deferred := watchFixtureDoors(rt)

	result := rt.HandleTargetInteract(testDivision, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	_, damage, fatal := assertSkillDamageOpen(
		t, result.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid,
	)
	if damage == 0 || fatal {
		t.Fatalf("base strike = damage %d fatal %v, want positive nonfatal", damage, fatal)
	}
	if !immediate.took("player-basic-attack") {
		t.Fatalf("handler engage committed through %v, want the immediate player-basic-attack door", immediate.labels)
	}
	if deferred.took("player-basic-attack") {
		t.Fatalf("handler engage deferred the stage commit (immediate %v, deferred %v); its answer would precede the commit", immediate.labels, deferred.labels)
	}
}

/*
================
TestTickSkillPathsTakeDeferredDoors

The engage's continuation is the tick's: the intent advance strikes
again through the deferred many-door, and the stage commit's label
never appears on an immediate seam.
================
*/
func TestTickSkillPathsTakeDeferredDoors(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100000)
	installStoreDoor(t, rt)
	immediate, deferred := watchFixtureDoors(rt)

	rt.HandleTargetInteract(testDivision, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	immediate.labels, deferred.labels = nil, nil

	// The continuation fires once the first swing's bracket has closed
	// and its NextActionMs has passed; step the tick until it does.
	for step := 1; step <= 40 && !deferred.took("player-basic-attack"); step++ {
		rt.TickHook()(clock.At(time.Duration(step) * 100 * time.Millisecond).UnixMilli())
	}
	if !deferred.took("player-basic-attack") {
		t.Fatalf("tick continuation committed through deferred %v (immediate %v), want the deferred player-basic-attack door", deferred.labels, immediate.labels)
	}
	if immediate.took("player-basic-attack") {
		t.Fatalf("tick continuation took the immediate player-basic-attack door %v; every tick hit would commit its own transaction", immediate.labels)
	}
}

/*
================
TestHandlerCommandsNeverTakeADeferredDoor

The lane rule as one table (#2036 review): whatever a target-interact
handler answers - an engage, or a follow that walks into range and
strikes - commits through the immediate doors and touches no deferred
seam, because its answer would otherwise precede the commit.
================
*/
func TestHandlerCommandsNeverTakeADeferredDoor(t *testing.T) {
	for _, command := range []struct {
		name  string
		setup func(t *testing.T) (*Runtime, *enterworld.Character, []byte)
	}{
		{"engage", func(t *testing.T) (*Runtime, *enterworld.Character, []byte) {
			rt, _, character, target := newCombatTestRuntime(t, 100000)
			return rt, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()
		}},
		{"follow", func(t *testing.T) (*Runtime, *enterworld.Character, []byte) {
			rt, _, character, target := followFixture(t)
			return rt, character, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode()
		}},
	} {
		t.Run(command.name, func(t *testing.T) {
			rt, character, payload := command.setup(t)
			installStoreDoor(t, rt)
			immediate, deferred := watchFixtureDoors(rt)

			rt.HandleTargetInteract(testDivision, character, payload)
			if len(immediate.labels) == 0 {
				t.Fatal("handler committed nothing; the table no longer reaches a commit")
			}
			if len(deferred.labels) != 0 {
				t.Fatalf("handler took deferred door(s) %v (immediate %v); its answer would precede the commit", deferred.labels, immediate.labels)
			}
		})
	}
}
