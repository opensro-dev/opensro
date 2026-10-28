/*
===========================================================================

tickdoor_test.go - tick paths take the deferred doors (#2028)

A tick path that takes an immediate door commits a SQLite transaction
per hit on the tick goroutine (live profile 13:56Z: commitTxLocked was
~12% of tick CPU through acceptSkillAreaAt, acceptSkillStagePhaseAt,
grantPickupTo and friends). The deferred doors join the tick's one
batched commit instead (store tickbatch.go).

These guards are behavioural: they poison the immediate seams and
drive the real paths. A converted path that reaches for an immediate
door again - or a new tick path that does - fails here by label.

===========================================================================
*/

package action

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
immediateDoorWatch

Records every label that arrives through the immediate Update seams
while delegating to the real door, so the scenario runs unchanged and
the guard reports at the end.
================
*/
type immediateDoorWatch struct {
	labels []string
}

/*
================
failIfUsed
================
*/
func (w *immediateDoorWatch) failIfUsed(t *testing.T) {
	t.Helper()
	if len(w.labels) != 0 {
		t.Fatalf("tick path took immediate door(s) %v, want the deferred doors (#2028)", w.labels)
	}
}

/*
================
TestTickPickupPathsTakeDeferredDoors

Through the real authority store with a tick batch open: a gold pickup
grant and a pickup approach arm must leave their rows to the batch.
The flush then commits them - a reboot reads the granted gold.
================
*/
func TestTickPickupPathsTakeDeferredDoors(t *testing.T) {
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	deps := d.rt.deps.(*enterworld.Deps)
	watch := &immediateDoorWatch{}
	deps.UpdateCharacter = func(c *enterworld.Character, label string, update func() bool) bool {
		watch.labels = append(watch.labels, label)
		return d.authority.UpdateCharacter(c, label, update)
	}
	deps.UpdateCharacters = func(cs []*enterworld.Character, label string, update func() bool) bool {
		watch.labels = append(watch.labels, label)
		return d.authority.UpdateCharacters(cs, label, update)
	}
	deps.UpdateCharacterDeferred = d.authority.UpdateCharacterDeferred
	deps.UpdateCharactersDeferred = d.authority.UpdateCharactersDeferred

	// Setup (not under guard): a gold heap at the character's feet and
	// one 100u east, out of reach.
	d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGoldDrop, GoldAmount: 1500,
	}))
	heap := groundByRefObjID(d.rt, 62)
	if heap == nil {
		t.Fatal("gold drop setup failed")
	}
	start := simulation.SeedWorldState(d.character).Spawn
	far := d.rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", d.clock.Now()))
	watch.labels = nil

	d.authority.BeginTickBatch()
	grant := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: heap.Gid}.Encode())
	assertOpcodes(t, grant.Frames,
		wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpPointsUpdate, wire.OpObjectDespawn, wire.OpActionState)
	armed := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: far.Gid}.Encode())
	if armed.Pending == nil {
		t.Fatalf("interact = %+v, want an armed approach", armed)
	}
	stats := d.authority.FlushTickBatch()
	watch.failIfUsed(t)
	if stats.Err != nil || !stats.Flushed || stats.Deferred == 0 {
		t.Fatalf("tick flush = %+v, want a flushed batch covering deferred doors", stats)
	}

	r := d.reboot(t)
	if got := goldOfT(t, r.character); got != 5000 {
		t.Fatalf("gold after reboot = %d, want the re-credited 5000", got)
	}
}

/*
================
TestTickSkillPathsTakeDeferredDoors

A basic-attack engage against the combat fixture: the stage commit
(player-basic-attack), the approach and the range entry must all take
the deferred seams. The immediate seams are poisoned after the
fixture's own door install, so any of them firing fails by label.
================
*/
func TestTickSkillPathsTakeDeferredDoors(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	watch := &immediateDoorWatch{}
	installStoreDoor(t, rt)
	immediate := deps.UpdateCharacter
	deps.UpdateCharacter = func(c *enterworld.Character, label string, update func() bool) bool {
		watch.labels = append(watch.labels, label)
		return immediate(c, label, update)
	}
	deps.UpdateCharacters = func(cs []*enterworld.Character, label string, update func() bool) bool {
		watch.labels = append(watch.labels, label)
		return update()
	}
	deferredMany := 0
	deps.UpdateCharactersDeferred = func(cs []*enterworld.Character, label string, update func() bool) bool {
		deferredMany++
		return update()
	}
	deps.UpdateCharacterDeferred = func(c *enterworld.Character, label string, update func() bool) bool {
		return update()
	}

	result := rt.HandleTargetInteract(testDivision, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	_, damage, fatal := assertSkillDamageOpen(
		t, result.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid,
	)
	if damage == 0 || fatal {
		t.Fatalf("base strike = damage %d fatal %v, want positive nonfatal", damage, fatal)
	}
	watch.failIfUsed(t)
	if deferredMany == 0 {
		t.Fatal("the stage commit never took the deferred many-door")
	}
}
