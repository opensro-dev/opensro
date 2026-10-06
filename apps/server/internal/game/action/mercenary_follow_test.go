/*
===========================================================================

mercenary_follow_test.go - live formation, speed, retirement and relocation

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"strings"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
mercenaryFormationFixture
================
*/
func mercenaryFormationFixture(t *testing.T) *doorRuntime {
	t.Helper()
	d, item := mercenaryFixture(t)
	d.rt.CompanionSurfaceHeight = func(_ uint16, _ float64, y float64, _ float64) (float64, bool) { return y, true }
	d.rt.ConstrainMovement = func(_ string, _ simulation.Spawn, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return to, nil
	}
	d.rt.CombatRoll = func() (uint32, error) { return 0, nil }
	useSummonerFixture(t, d.rt, d.character, 23, item)
	owner := simulation.Spawn{RegionID: 0x62a8, X: 500, Z: 500}
	d.rt.Worlds.Update(simulation.WorldKey(testDivision, d.character.Name), func() simulation.WorldState { return simulation.SeedWorldState(d.character) }, func(w *simulation.WorldState) {
		w.Spawn = owner
		w.MoveSegment = nil
		w.Walk = 20
		w.Run = 100
		w.MovementMode = simulation.RunMode
	})
	for _, pet := range d.character.Mercenaries {
		ref, ok := d.rt.cosReference(pet)
		if !ok {
			t.Fatal("missing reference")
		}
		ref.Parameters.BodyRadius = 10
		state := d.rt.petSessionFor(testDivision, d.character.Name, pet.GID)
		from := owner
		from.X -= 300
		state.follower = simulation.NewPetFollower(pet.GID, from)
	}
	return d
}

/*
================
TestMercenaryFormationLifecycleAndSpeedPublication
================
*/
func TestMercenaryFormationLifecycleAndSpeedPublication(t *testing.T) {
	d := mercenaryFormationFixture(t)
	c := d.character
	owner := d.rt.petSessionFor(testDivision, c.Name, 0)
	for _, pet := range c.Mercenaries {
		key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: pet.GID}
		frames := d.rt.advancePet(key, 1000000)
		if len(frames) < 2 {
			t.Fatal("formation did not move", frames)
		}
		state := d.rt.petSessionFor(testDivision, c.Name, pet.GID)
		before, _ := state.follower.Presentation()
		if !before.MoveSegment.Valid() || !pet.FollowRunSet {
			t.Fatal("movement/source missing", before, pet.FollowRunSet)
		}
		if frames := d.rt.advancePet(key, 1000099); len(frames) != 0 {
			t.Fatal("follow cadence fired early", frames)
		}
		_, run := d.rt.EntryCompanionMovementSpeeds(testDivision, c, pet)
		peer := d.rt.companionPresentation(testDivision, state, pet)
		if run != 125 || peer.Row.Run != run || before.Run != run {
			t.Fatalf("speed disagrees: entry=%v peer=%v mover=%v", run, peer.Row.Run, before.Run)
		}
	}
	count := 0
	for _, gid := range owner.formationSlots {
		if gid != 0 {
			count++
		}
	}
	if count != 6 {
		t.Fatal("not every soldier reserved a slot", owner.formationSlots)
	}
	d.rt.HandleMercenaryDismiss(testDivision, c, nil)
	if owner.formationSlots != (monster.ApproachSlots{}) {
		t.Fatal("dismiss leaked slots", owner.formationSlots)
	}
}

/*
================
TestMercenaryFormationRelocationPublishesNewVisibilityGeneration
================
*/
func TestMercenaryFormationRelocationPublishesNewVisibilityGeneration(t *testing.T) {
	d := mercenaryFormationFixture(t)
	pet := d.character.Mercenaries[0]
	state := d.rt.petSessionFor(testDivision, d.character.Name, pet.GID)
	state.follower = simulation.NewPetFollower(pet.GID, simulation.Spawn{RegionID: 0x62a5, X: 200, Z: 500})
	generation := state.generation
	frames := d.rt.advancePet(petOwnerKey{division: testDivision, name: strings.ToLower(d.character.Name), gid: pet.GID}, 1000000)
	if state.generation != generation+1 || len(frames) != 2 || frames[0].Opcode != wire.OpObjectDespawn || frames[1].Opcode != wire.OpSingleObjectSpawn {
		t.Fatal("relocation did not replace visibility", frames, state.generation)
	}
	if payload := frames[1].Payload; payload[len(payload)-1] != 7 {
		t.Fatal("missing reason 7", payload)
	}
	view := d.rt.companionPresentation(testDivision, state, pet)
	if view.Row.State != 7 || view.Generation != state.generation || view.World.Spawn.RegionID != 0x62a8 {
		t.Fatal("peer relocation disagrees", view)
	}
}

/*
================
TestMercenaryFormationCompletionAndDeathReleaseSlots
================
*/
func TestMercenaryFormationCompletionAndDeathReleaseSlots(t *testing.T) {
	d := mercenaryFormationFixture(t)
	pet := d.character.Mercenaries[0]
	key := petOwnerKey{division: testDivision, name: strings.ToLower(d.character.Name), gid: pet.GID}
	state := d.rt.petSessionFor(testDivision, d.character.Name, pet.GID)
	owner := d.rt.petSessionFor(testDivision, d.character.Name, 0)
	d.rt.advancePet(key, 1000000)
	state.follower.Displace(simulation.Spawn{RegionID: 0x62a8, X: 440, Z: 500}, 1000050)
	d.rt.advancePet(key, 1000100)
	if owner.formationSlots != (monster.ApproachSlots{}) {
		t.Fatal("exactly 60 retained reservation", owner.formationSlots)
	}
	state.follower.Displace(simulation.Spawn{RegionID: 0x62a8, X: 200, Z: 500}, 1000150)
	d.rt.advancePet(key, 1000200)
	d.rt.deps.Mutate(d.character, "test-soldier-death", func() { pet.CurrentHP = 0 })
	d.rt.advancePet(key, 1000300)
	if owner.formationSlots != (monster.ApproachSlots{}) || state.formationActive {
		t.Fatal("death retained follow state", owner.formationSlots)
	}
}

/*
================
TestMercenaryFormationBattleTimerSurvivesFollowReentry
================
*/
func TestMercenaryFormationBattleTimerSurvivesFollowReentry(t *testing.T) {
	d := mercenaryFormationFixture(t)
	pet := d.character.Mercenaries[0]
	state := d.rt.petSessionFor(testDivision, d.character.Name, pet.GID)
	ref, _ := d.rt.cosReference(pet)
	key := petOwnerKey{division: testDivision, name: strings.ToLower(d.character.Name), gid: pet.GID}
	step := petCombatStep{key: key, state: state, snapshot: d.character.Snapshot(), pet: pet, ref: ref, run: 60, nowMs: 1000000}
	d.rt.advanceCompanionBattleFollowTimer(step)
	d.rt.releasePetFormation(key, state)
	before, _ := state.follower.Presentation()
	d.rt.advancePet(key, 1000050)
	after, _ := state.follower.Presentation()
	if after.MoveSegment != before.MoveSegment {
		t.Fatal("unexpired battle timer lost on follow entry")
	}
	d.rt.advancePet(key, 1000150)
	after, _ = state.follower.Presentation()
	if !after.MoveSegment.Valid() {
		t.Fatal("expired follow timer did not move")
	}
}

/*
================
TestMercenaryFormationSharesSlotsWithAttackAndPickupPets
================
*/
func TestMercenaryFormationSharesSlotsWithAttackAndPickupPets(t *testing.T) {
	for _, band := range []uint16{3, 4} {
		d := mercenaryFormationFixture(t)
		c := d.character
		refs := d.rt.deps.ItemReferences().(cosTestItemSource)
		refs.characters["EXTRA_PET"] = &enterworld.CharacterRef{Codename: "EXTRA_PET", RefObjID: 9999, TidWord: 0x1c6 | band<<11, MaxHP: 100, RunSpeed: 80, WalkSpeed: 20}
		pet := &enterworld.CharacterCOS{Codename: "EXTRA_PET", RefObjID: 9999, GID: 123456, CurrentHP: 100, Summoned: true}
		c.ActiveCOS = pet
		owner := d.rt.petSessionFor(testDivision, c.Name, 0)
		state := d.rt.bindCompanionSession(testDivision, owner, pet)
		state.follower = simulation.NewPetFollower(pet.GID, simulation.Spawn{RegionID: 0x62a8, X: 200, Z: 500})
		for _, cos := range c.Companions() {
			d.rt.advancePet(petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: cos.GID}, 1000000)
		}
		count := 0
		found := false
		for _, gid := range owner.formationSlots {
			if gid != 0 {
				count++
			}
			found = found || gid == pet.GID
		}
		if count != 7 || !found {
			t.Fatal("pet used a separate formation", band, owner.formationSlots)
		}
	}
}
