/*
===========================================================================

cosabnormal_mechanics_test.go - independent summoned-character status effects

Exercise the live adapter, authority commit and packet publication. A mask
alone is insufficient evidence that a pet actually takes damage or slows.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCosPeriodicDamageEchoOnlyReachesCreditedPlayer

The rider is not the native player victim. Only a living credited source
gets this private echo, and overkill preserves the authored damage word.
================
*/
func TestCosPeriodicDamageEchoOnlyReachesCreditedPlayer(t *testing.T) {
	for _, departed := range []bool{false, true} {
		for _, damage := range []uint32{7, 300} {
			rt, clock, rider, _ := newCombatTestRuntime(t, 100)
			equipCombatTestPet(t, rt, rider, 4)
			rider.ActiveCOS.CurrentHP = 10
			source := *rider
			source.ID, source.Name, source.ActiveCOS = rider.ID+1, "PeriodicCaster", nil
			characters := rt.deps.(*enterworld.Deps).Characters.(enterworld.StaticCharacterSource)
			characters[testDivision] = append(characters[testDivision], &source)
			record := abnormal.Record{Status: abnormal.Burn, Level: 1, DurationMs: 10000,
				SourceGID: enterworld.ObjectIDForCharacter(&source), SourceName: source.Name,
				Rate24: damage, Scale20: 1}
			owner := rt.newCosAbnormalOwner(testDivision, rider, clock.NowMs())
			owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
			if !owner.block.Apply(owner, record, clock.NowMs()) {
				t.Fatal("burn did not install")
			}
			owner.commit()
			if departed {
				characters[testDivision] = characters[testDivision][:1]
			}
			batches := rt.advanceCosAbnormals(clock.NowMs())
			count := 0
			for _, batch := range batches {
				for _, frame := range batch.Frames {
					if frame.Opcode != abnormalDamageCreditOpcode {
						continue
					}
					count++
					if batch.OnlyCharacterID != source.ID || len(frame.Payload) != 8 ||
						binary.LittleEndian.Uint32(frame.Payload) != rider.ActiveCOS.GID ||
						binary.LittleEndian.Uint32(frame.Payload[4:]) != damage {
						t.Fatalf("departed %v damage %d: recipient %d payload %x", departed, damage, batch.OnlyCharacterID, frame.Payload)
					}
				}
			}
			want := 1
			if departed {
				want = 0
			}
			if count != want || rider.ActiveCOS.CurrentHP != uint32(max(0, 10-int64(damage))) {
				t.Fatalf("departed %v damage %d: echoes %d hp %d", departed, damage, count, rider.ActiveCOS.CurrentHP)
			}
		}
	}
}

/*
================
TestCosPeriodicStatusesDebitIndependentVitals
================
*/
func TestCosPeriodicStatusesDebitIndependentVitals(t *testing.T) {
	cases := []struct {
		status abnormal.Status
		hp, mp uint32
	}{
		{abnormal.Burn, 490, 300}, {abnormal.Poison, 490, 300}, {abnormal.Bleeding, 490, 300},
		{abnormal.Panic, 500, 220}, {abnormal.Combustion, 500, 252},
	}
	for _, tc := range cases {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		ref := equipCombatTestPet(t, rt, character, 4)
		ref.MaxHP = 1000
		character.ActiveCOS.CurrentHP, character.ActiveCOS.CurrentMP = 500, 300
		record := abnormal.Record{Status: tc.status, Level: 1, Grade: 1, DurationMs: 10000,
			PeriodMs: 2000, SourceGID: source.Gid, Rate24: 10, Scale20: 1, Param38: 10, Param2C: 20}
		owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
		owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
		if !owner.block.Apply(owner, record, clock.NowMs()) {
			t.Fatalf("status %d did not install", tc.status)
		}
		owner.commit()
		frames := rt.advanceCosAbnormals(clock.NowMs())
		pet := character.ActiveCOS
		if pet.CurrentHP != tc.hp || pet.CurrentMP != tc.mp || len(frames) == 0 {
			t.Errorf("status %d: pet HP/MP %d/%d, want %d/%d; packets %d", tc.status, pet.CurrentHP, pet.CurrentMP, tc.hp, tc.mp, len(frames))
		}
		if enterworld.CurrentHP(character) != 100 {
			t.Errorf("status %d damaged the rider", tc.status)
		}
		rt.advanceCosAbnormals(clock.NowMs() + 2000)
		if pet.CurrentHP != tc.hp || pet.CurrentMP != tc.mp {
			t.Errorf("status %d ticked at period equality", tc.status)
		}
	}
}

/*
================
TestCosStatusImmunityAndElementResistance
================
*/
func TestCosStatusImmunityAndElementResistance(t *testing.T) {
	for _, band := range []uint16{1, 2, 3, 4} {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		ref := equipCombatTestPet(t, rt, character, band)
		ref.Parameters.ElementResist = [6]uint8{10, 20, 30, 40, 50, 60}
		rt.CombatRoll = func() (uint32, error) { return 0, nil }
		var params abnormal.SkillParams
		for index := range 6 {
			params.Params[index] = abnormal.Param{Present: true, Args: [6]uint32{100, 100, 1}}
		}
		owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
		records, err := rt.rollMonsterOnCOS(cosAbnormalRoll{division: testDivision, caster: source, params: &params, target: owner})
		if err != nil {
			t.Fatal(err)
		}
		if band == 1 {
			if len(records) != 0 {
				t.Fatal("attack COS accepted an abnormal status")
			}
			continue
		}
		if len(records) != 6 {
			t.Fatalf("band %d rolled %d elements", band, len(records))
		}
		for index, record := range records {
			if record.Level != uint16(90-index*10) {
				t.Errorf("band %d element %d power %d", band, index, record.Level)
			}
		}
	}
}

/*
================
TestCosFreezeStopsFollowerAndBlocksFurtherMovement
================
*/
func TestCosFreezeStopsFollowerAndBlocksFurtherMovement(t *testing.T) {
	rt, clock, character, source := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, character, 4)
	rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	rt.BindPetSession(testDivision, character, 1)
	now := clock.NowMs()
	rt.advancePets(now)
	rt.Worlds.Update(simulation.WorldKey(testDivision, character.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(character) },
		func(world *simulation.WorldState) { world.Spawn.X += 200 })
	rt.advancePets(now + 1)
	before := rt.PetPresentation(testDivision, character.Name)
	if before == nil || before.World.MoveSegment == nil {
		t.Fatal("fixture pet did not start moving")
	}
	record := abnormal.Record{Status: abnormal.Freeze, Level: 1, DurationMs: 10000, SourceGID: source.Gid}
	owner := rt.newCosAbnormalOwner(testDivision, character, now+501)
	owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
	owner.changed = owner.block.Apply(owner, record, now+501)
	owner.commit()
	frames := rt.cosAbnormalPublication(character.ActiveCOS.GID, owner)
	if len(frames) == 0 {
		t.Fatal("freeze did not publish its stop and mask")
	}
	rt.advancePets(now + 1001)
	after := rt.PetPresentation(testDivision, character.Name)
	if after == nil || after.World.MoveSegment != nil || after.World.Spawn != before.World.LiveSpawnAt(now+501) {
		t.Fatalf("frozen pet drifted: %+v", after)
	}
	if len(after.AbnormalVitals) < 11 || binary.LittleEndian.Uint32(after.AbnormalVitals[7:]) != abnormal.Freeze.Bit() {
		t.Fatal("late observer snapshot lost pet freeze")
	}
}

/*
================
TestCosFatalStatusClearsLifeAndMountOnce

Burn and poison share the debit adapter, but poison must retain one HP.
Death clears the persisted revival guard and publishes the common LIFE edge.
================
*/
func TestCosFatalStatusClearsLifeAndMountOnce(t *testing.T) {
	for _, status := range []abnormal.Status{abnormal.Burn, abnormal.Bleeding, abnormal.Poison} {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		equipCombatTestPet(t, rt, character, 2)
		pet := character.ActiveCOS
		pet.CurrentHP, pet.Mounted = 5, true
		record := abnormal.Record{Status: status, Level: 1, Grade: 1, DurationMs: 10000,
			PeriodMs: 2000, SourceGID: source.Gid, Rate24: 10, Scale20: 1, Param38: 10}
		owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
		owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
		if !owner.block.Apply(owner, record, clock.NowMs()) {
			t.Fatal("status refused")
		}
		owner.block.Update(owner, clock.NowMs())
		owner.commit()
		frames := rt.cosAbnormalPublication(pet.GID, owner)
		if status == abnormal.Poison {
			if pet.CurrentHP != 1 || !pet.Mounted || pet.StateFlags&1 == 0 || owner.died {
				t.Fatal("poison killed or dismounted the pet")
			}
			continue
		}
		if pet.CurrentHP != 0 || pet.Mounted || pet.StateFlags&1 != 0 || !owner.died || owner.block.Mask != 0 {
			t.Fatalf("status %d left a partial death: %+v", status, pet)
		}
		if !saw(frames, wire.OpObjectStateRefresh) || !saw(frames, wire.OpCosRideState) {
			t.Fatal("death did not notify observers")
		}
		again := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs()+1)
		again.commit()
		if saw(rt.cosAbnormalPublication(pet.GID, again), wire.OpObjectStateRefresh) {
			t.Fatal("dead pet replayed LIFE transition")
		}
	}
}

/*
================
TestPetPotionUsesOwnRecoveryReductions

Each native pet-potion family uses instantaneous reduced recovery. The rider's
status keeper must not substitute for the target, including Zombie's MP arm.
================
*/
func TestPetPotionUsesOwnRecoveryReductions(t *testing.T) {
	for _, kind := range []int64{4, 5, 7} {
		for _, zombie := range []bool{false, true} {
			rt, clock, character, source := newCombatTestRuntime(t, 100)
			ref := equipCombatTestPet(t, rt, character, 4)
			ref.MaxHP, ref.MaxMP = 1000, 1000
			character.MissionInventory = nil
			pet := character.ActiveCOS
			pet.CurrentHP, pet.CurrentMP = 500, 100
			potion := *testItems()["ITEM_ETC_HP_POTION_01"]
			potion.TypeIDs[3] = kind
			potion.RecoveryHP, potion.RecoveryMP = 0, 0
			if kind != 5 {
				potion.RecoveryHP = 80
			}
			if kind != 4 {
				potion.RecoveryMP = 80
			}
			rt.deps.(*enterworld.Deps).Items.(cosTestItemSource).staticItemSource[potion.Codename] = &potion
			character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
				Slot: 21, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 1})
			owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
			records := []abnormal.Record{
				{Status: abnormal.Panic, Grade: 1, DurationMs: 10000, SourceGID: source.Gid, Param34: 50},
				{Status: abnormal.Combustion, Grade: 1, DurationMs: 10000, SourceGID: source.Gid, Param34: 25},
			}
			if zombie {
				records = append(records, abnormal.Record{Status: abnormal.Zombie, Level: 1, DurationMs: 10000, SourceGID: source.Gid})
			}
			owner.sources = rt.captureAbnormalSources(testDivision, owner.block, records)
			for _, record := range records {
				if !owner.block.Apply(owner, record, clock.NowMs()) {
					t.Fatal("status refused")
				}
			}
			owner.commit()
			stats, _, err := rt.playerCombatStats(testDivision, character)
			if err != nil {
				t.Fatal(err)
			}
			maxHP, maxMP, _, _ := rt.playerKeeperVitals(testDivision, character)
			amount, ok := computePotionAmount(&potion, stats.Level, stats.Strength, stats.Intellect, maxHP, maxMP)
			if !ok {
				t.Fatal("potion sizing failed")
			}
			hp, mp := int64(pet.CurrentHP), int64(pet.CurrentMP)
			wantHP, wantMP := hp+amount.hp/2, mp+amount.mp*3/4
			if zombie {
				wantHP = hp - amount.hp
			}
			result := rt.HandleItemUse(testDivision, character, petUse(character, &potion, pet.GID, -1))
			if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
				t.Fatalf("potion refused %+v", result)
			}
			if pet.CurrentHP != uint32(wantHP) || pet.CurrentMP != uint32(wantMP) {
				t.Errorf("family %d zombie %v: %d/%d want %d/%d", kind, zombie, pet.CurrentHP, pet.CurrentMP, wantHP, wantMP)
			}
		}
	}
}

/*
================
TestMountedCosUsesOwnMovementKeeper

Both ordinary movement admission and COS commands must observe the mounted
actor. Rider speed refreshes cannot overwrite a slowed vehicle's mover.
================
*/
func TestMountedCosUsesOwnMovementKeeper(t *testing.T) {
	rt, clock, c, source := newCombatTestRuntime(t, 100)
	ref := equipCombatTestPet(t, rt, c, 2)
	c.ActiveCOS.Mounted = true
	owner := rt.newCosAbnormalOwner(testDivision, c, clock.NowMs())
	record := abnormal.Record{Status: abnormal.Slow, Grade: 1, DurationMs: 10000, SourceGID: source.Gid}
	owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
	owner.changed = owner.block.Apply(owner, record, clock.NowMs())
	owner.commit()
	rt.refreshMovementEffects(testDivision, c, clock.NowMs())
	world := rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
	if world.Walk != ref.WalkSpeed*0.75 || world.Run != ref.RunSpeed*0.75 {
		t.Fatalf("mounted speed %v/%v", world.Walk, world.Run)
	}
	record = abnormal.Record{Status: abnormal.Freeze, Level: 1, DurationMs: 10000, SourceGID: source.Gid}
	owner.block.Apply(owner, record, clock.NowMs())
	owner.commit()
	if !rt.PlayerMovementBlocked(testDivision, c.Name) {
		t.Fatal("rider could bypass frozen COS using ordinary movement")
	}
	c.ActiveCOS.Mounted = false
	if rt.PlayerMovementBlocked(testDivision, c.Name) {
		t.Fatal("pet freeze blocked dismounted rider")
	}
}
