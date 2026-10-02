/*
===========================================================================

cosride_test.go - ride authority, native range and detached vehicle position

===========================================================================
*/
package action

import (
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCosRideSharesMountRangeAndPreservesDismountedVehicle
================
*/
func TestCosRideSharesMountRangeAndPreservesDismountedVehicle(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 100, Summoned: true}
	rt.BindPetSession(testDivision, c, 1)
	initial := rt.PetPresentation(testDivision, c.Name).World.Spawn
	key := simulation.WorldKey(testDivision, c.Name)
	mount := wire.NewWriter(5).U8(1).U32(gid).Payload()
	for _, pose := range []simulation.Spawn{
		{RegionID: initial.RegionID ^ 0x8000, X: initial.X, Y: initial.Y, Z: initial.Z},
		{RegionID: initial.RegionID, X: initial.X + 31, Y: initial.Y, Z: initial.Z},
		{RegionID: initial.RegionID, X: initial.X, Y: initial.Y + 31, Z: initial.Z},
	} {
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn = pose })
		if result := rt.HandleCosRide(testDivision, c, mount); !reflect.DeepEqual(result.Frames, cosRideFailure(cosRideOutOfRange).Frames) || len(result.Broadcast) != 0 || c.ActiveCOS.Mounted {
			t.Fatal("out-of-range mount accepted", pose, result)
		}
	}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = initial
		w.Spawn.X += cosMountRange
	})
	assertOpcodes(t, rt.HandleCosRide(testDivision, c, mount).Frames,
		wire.OpObjectSourceCorrection, wire.OpCosRideState, movementSpeedOpcode)
	if !c.ActiveCOS.Mounted {
		t.Fatal("native inclusive range boundary rejected")
	}
	if rider := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }); rider.Spawn != initial || rider.MoveSegment != nil {
		t.Fatal("mount did not relocate rider onto the admitted vehicle", rider)
	}
	if saved := simulation.SeedWorldState(c); saved.Spawn != initial {
		t.Fatal("mount relocation was not persisted", saved.Spawn)
	}
	if result := rt.HandleCosRide(testDivision, c, mount); !reflect.DeepEqual(result.Frames, cosRideFailure(cosRideInvalidState).Frames) || len(result.Broadcast) != 0 {
		t.Fatal("repeated mount changed state", result)
	}
	dismount := wire.NewWriter(5).U8(0).U32(gid).Payload()
	result := rt.HandleCosRide(testDivision, c, dismount)
	if c.ActiveCOS.Mounted || len(result.Frames) == 0 || !reflect.DeepEqual(result.Frames, result.Broadcast) {
		t.Fatal("dismount did not publish the committed state", result)
	}
	parked := rt.PetPresentation(testDivision, c.Name).World.Spawn
	if parked != initial {
		t.Fatal("dismount reverted transport to its summon position", parked)
	}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X += 200 })
	if after := rt.PetPresentation(testDivision, c.Name).World.Spawn; after != parked {
		t.Fatal("parked vehicle followed player", after, parked)
	}
	before := c.Snapshot()
	for _, bad := range [][]byte{{}, {1}, append(mount, 0), wire.NewWriter(5).U8(2).U32(gid).Payload(), wire.NewWriter(5).U8(1).U32(gid + 1).Payload()} {
		rt.HandleCosRide(testDivision, c, bad)
		if !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("malformed or foreign request mutated character", bad)
		}
	}
}

/*
================
TestCosMountCommandSharesPostureRefusal

Both native composers must reject the same posture without publishing a
ride change or relocating the owner.
================
*/
func TestCosMountCommandSharesPostureRefusal(t *testing.T) {
	for _, posture := range []uint8{6, 7} {
		for _, panel := range []bool{false, true} {
			c := testCharacter()
			rt, _ := newTestRuntime(c, testCosSource(testItems()))
			gid, _ := enterworld.CosObjectIDForCharacter(c)
			c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true}
			c.NativeBodyStatus = posture
			rt.BindPetSession(testDivision, c, 1)
			before := c.Snapshot()
			var result OpResult
			if panel {
				result = rt.HandleCosRide(testDivision, c, wire.NewWriter(5).U8(1).U32(gid).Payload())
			} else {
				result = rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(gid).U8(wire.CosCommandMountTag).Payload())
			}
			if !reflect.DeepEqual(result.Frames, cosRideFailure(cosRideInvalidPosture).Frames) || len(result.Broadcast) != 0 || !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatalf("posture %d panel %v changed state: %+v", posture, panel, result)
			}
		}
	}
}

/*
================
TestCosMountCannotInterruptCommittedAttack

The real skill owner supplies the busy state; no test-only mount flag can
stand in for the casting instance checked by the native manager. The
attack also starts the battle state, which the 74B5 door (5119FA) refuses
first with 0xB; it is cleared here so both doors reach the busy check.
================
*/
func TestCosMountCannotInterruptCommittedAttack(t *testing.T) {
	for _, panel := range []bool{false, true} {
		rt, _, c, target := newCombatTestRuntime(t, 100000)
		originalItems := rt.deps.ItemReferences().(staticItemSource)
		equipCombatTestPet(t, rt, c, 2)
		refs := rt.deps.ItemReferences().(cosTestItemSource)
		refs.staticItemSource = originalItems
		rt.deps.(*enterworld.Deps).Items = refs
		skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		c.CurrentMP = testInt64(10000)
		started := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
		if !rt.PlayerAttackLocked(testDivision, c.Name) {
			t.Fatal("fixture did not open a casting instance", started)
		}
		c.BattleUntilMs = 0
		before := c.Snapshot()
		var result OpResult
		if panel {
			result = rt.HandleCosRide(testDivision, c, wire.NewWriter(5).U8(1).U32(c.ActiveCOS.GID).Payload())
		} else {
			result = rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(c.ActiveCOS.GID).U8(wire.CosCommandMountTag).Payload())
		}
		if !reflect.DeepEqual(result.Frames, cosRideFailure(cosRideBusy).Frames) || len(result.Broadcast) != 0 || !reflect.DeepEqual(before, c.Snapshot()) || !rt.PlayerAttackLocked(testDivision, c.Name) {
			t.Fatal("mount interrupted committed attack", panel, result)
		}
	}
}

/*
================
TestCosMountRefusedInBattle

5119FA: a rider in battle state is refused with 0xB before the vehicle is
examined; the same request mounts once the battle state lapses, and getting
off is never refused for battle.
================
*/
func TestCosMountRefusedInBattle(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 100, Summoned: true}
	rt.BindPetSession(testDivision, c, 1)
	mount := wire.NewWriter(5).U8(1).U32(gid).Payload()

	c.BattleUntilMs = clock.Now().UnixMilli() + battleStateMs
	before := c.Snapshot()
	result := rt.HandleCosRide(testDivision, c, mount)
	if !reflect.DeepEqual(result.Frames, cosRideFailure(cosRideInBattle).Frames) || len(result.Broadcast) != 0 ||
		!reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("mount in battle was not refused with 0xB", result)
	}

	c.BattleUntilMs = 0
	assertOpcodes(t, rt.HandleCosRide(testDivision, c, mount).Frames,
		wire.OpObjectSourceCorrection, wire.OpCosRideState, movementSpeedOpcode)
	if !c.ActiveCOS.Mounted {
		t.Fatal("mount after battle refused")
	}

	c.BattleUntilMs = clock.Now().UnixMilli() + battleStateMs
	if result := rt.HandleCosRide(testDivision, c, wire.NewWriter(5).U8(0).U32(gid).Payload()); c.ActiveCOS.Mounted || len(result.Broadcast) == 0 {
		t.Fatal("dismount in battle refused", result)
	}
}

/*
================
TestCosMountRefusedWhileTheRiderHasAMotion

5119D5: a set motion byte (GetMotionState 4AA590) refuses the mount with 5
before the battle check. Moving, seated, changing posture and the 1.5 s thaw
the freeze leaves behind each hold it; the same request mounts once the
byte is clear.
================
*/
func TestCosMountRefusedWhileTheRiderHasAMotion(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 100, Summoned: true}
	rt.BindPetSession(testDivision, c, 1)
	key := simulation.WorldKey(testDivision, c.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(c) }
	vehicle := rt.PetPresentation(testDivision, c.Name).World.Spawn
	mount := wire.NewWriter(5).U8(1).U32(gid).Payload()
	now := clock.Now().UnixMilli()

	for name, pose := range map[string]func(*simulation.WorldState){
		"moving": func(w *simulation.WorldState) {
			w.MoveSegment = &simulation.MoveSegment{From: vehicle, StartedAtMs: now - 100, ArrivesAtMs: now + 1000}
		},
		"seated":   func(w *simulation.WorldState) { w.Sitting = true },
		"standing": func(w *simulation.WorldState) { w.PostureTransitionUntilMs = now + 500 },
	} {
		rt.Worlds.Update(key, seed, func(w *simulation.WorldState) {
			*w = simulation.SeedWorldState(c)
			w.Spawn = vehicle
			pose(w)
		})
		// Battle is also set: the motion refusal comes first.
		c.BattleUntilMs = now + battleStateMs
		if result := rt.HandleCosRide(testDivision, c, mount); !reflect.DeepEqual(result.Frames, cosRideFailure(cosRideInvalidState).Frames) || c.ActiveCOS.Mounted {
			t.Fatal(name, "mount was not refused with 5", result)
		}
	}
	c.BattleUntilMs = 0
	rt.Worlds.Update(key, seed, func(w *simulation.WorldState) {
		*w = simulation.SeedWorldState(c)
		w.Spawn = vehicle
	})

	// A freeze ending installs the thaw (0xA, then 0 after 1.5 s).
	owner := &playerAbnormalOwner{rt: rt, division: testDivision, c: c, block: &abnormal.Block{}, now: now}
	owner.SetMotion(0xa, 0, 1.5)
	if result := rt.HandleCosRide(testDivision, c, mount); !reflect.DeepEqual(result.Frames, cosRideFailure(cosRideInvalidState).Frames) || c.ActiveCOS.Mounted {
		t.Fatal("mount during the thaw was not refused with 5", result)
	}
	clock.Advance(1500 * time.Millisecond)
	assertOpcodes(t, rt.HandleCosRide(testDivision, c, mount).Frames,
		wire.OpObjectSourceCorrection, wire.OpCosRideState, movementSpeedOpcode)
	if !c.ActiveCOS.Mounted {
		t.Fatal("mount after the thaw refused")
	}
}
