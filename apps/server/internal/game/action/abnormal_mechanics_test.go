/*
===========================================================================

abnormal_mechanics_test.go - status consequences at the gameplay boundary

Exercise keeper resistance, live movement and publication together. Testing
the abnormal block alone cannot detect an adapter that drops its callbacks.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"math"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPeriodicDamageEchoAlwaysReachesVictim

52A38E and 52A3FD both notify the victim. Source credit is an additional
private recipient; it cannot suppress an uncredited or lethal hit's echo.
================
*/
func TestPeriodicDamageEchoAlwaysReachesVictim(t *testing.T) {
	for _, credited := range []bool{false, true} {
		for _, damage := range []uint32{7, 300} {
			rt, clock, c, source := newCombatTestRuntime(t, 100)
			c.CurrentHP = testInt64(10)
			owner := rt.newPlayerAbnormalOwner(testDivision, c, clock.NowMs())
			owner.Hit(source.Gid, credited, damage, abnormalDamageOverTimeReason, abnormal.Burn)
			frames := rt.playerAbnormalPublication(testDivision, c, owner)
			count := 0
			for _, frame := range frames.actor {
				if frame.Opcode != abnormalDamageCreditOpcode {
					continue
				}
				count++
				if len(frame.Payload) != 8 || binary.LittleEndian.Uint32(frame.Payload) != enterworld.ObjectIDForCharacter(c) ||
					binary.LittleEndian.Uint32(frame.Payload[4:]) != damage {
					t.Fatalf("credited %v damage %d: payload %x", credited, damage, frame.Payload)
				}
			}
			for _, frame := range frames.public {
				if frame.Opcode == abnormalDamageCreditOpcode {
					t.Fatal("private damage echo leaked to observers")
				}
			}
			if count != 1 || enterworld.CurrentHP(c) != max(0, 10-int64(damage)) {
				t.Fatalf("credited %v damage %d: echoes %d hp %d", credited, damage, count, enterworld.CurrentHP(c))
			}
		}
	}
}

/*
================
TestPeriodicSelfSourceCannotDebitItsOwner
================
*/
func TestPeriodicSelfSourceCannotDebitItsOwner(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	owner := rt.newPlayerAbnormalOwner(testDivision, c, clock.NowMs())
	before := enterworld.CurrentHP(c)
	owner.Hit(enterworld.ObjectIDForCharacter(c), true, 10, abnormalDamageOverTimeReason, abnormal.Burn)
	if enterworld.CurrentHP(c) != before || len(owner.hits) != 0 {
		t.Fatal("52A288 self-source guard was bypassed")
	}
}

/*
================
TestWallStatusContextThroughMonsterAttack

Walls have a status context even when the damage lane passes through them.
Stun is excluded by either wall; a magical wall also suppresses ordinary
status rolls unless the attacking skill carries the native stun parameter.
================
*/
func TestWallStatusContextThroughMonsterAttack(t *testing.T) {
	for _, wallID := range []uint32{crystalWallA1, fireWallA1} {
		for _, stun := range []bool{false, true} {
			rt, clock, c, mob := wallFixture(t, wallID, 5)
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			skills := rt.deps.SkillData().(staticSkillSource)
			row := skills[2]
			row.Attack.Min, row.Attack.Max = 1, 1
			burnIndex, _ := abnormal.SourceIndex(0x6275)
			row.Abnormal.Params[burnIndex] = abnormal.Param{Present: true, Args: [6]uint32{10, 100, 1}}
			if stun {
				stunIndex, _ := abnormal.SourceIndex(0x7374)
				row.Abnormal.Params[stunIndex] = abnormal.Param{Present: true, Args: [6]uint32{1000, 100, 1}}
			}
			skills[2] = row
			result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if !result.Accepted {
				t.Fatalf("wall %d stun %v: refused %+v", wallID, stun, result)
			}
			mask := uint32(0)
			if block := rt.playerAbnormal(testDivision, c.Name); block != nil {
				mask = block.Mask
			}
			wantBurn := wallID == crystalWallA1 || stun
			if mask&abnormal.Stun.Bit() != 0 || (mask&abnormal.Burn.Bit() != 0) != wantBurn {
				t.Fatalf("wall %d stun %v: mask %x; want burn %v and no stun", wallID, stun, mask, wantBurn)
			}
		}
	}
}

/*
================
TestElementResistanceKeepsNativeParameterOrder

Give every element a different resistance. Equal burn and shock values hide
the slot-order versus keeper-order mismatch that this regression exercises.
================
*/
func TestElementResistanceKeepsNativeParameterOrder(t *testing.T) {
	rt, _, character, attacker := newCombatTestRuntime(t, 100)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	var params abnormal.SkillParams
	var modifiers []paramkeeper.Write
	for index := range 6 {
		params.Params[index] = abnormal.Param{Present: true, Args: [6]uint32{100, 100, 1}}
		modifiers = append(modifiers, paramkeeper.Write{
			Parameter: uint16(0x1b + index), Source: 99, Value: float32(index * 10),
		})
		attacker.Ref.ElementResist[index] = uint8(index * 10)
	}
	stats, _, err := combat.PlayerStatsWithModifiers(character, rt.statCatalogs(), modifiers, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, err := rt.rollMonsterOnPlayer(testDivision, attacker, &params, character, stats, nil)
	if err != nil || len(records) != 6 {
		t.Fatalf("element rolls: %+v, %v", records, err)
	}
	context := monsterAbnormalContext{rt: rt}
	for index, record := range records {
		if record.Status != abnormal.Sources[index].Status || record.Level != uint16(100-index*10) {
			t.Errorf("element %d: status %d, power %d; want status %d, power %d", index,
				record.Status, record.Level, abnormal.Sources[index].Status, 100-index*10)
		}
		if got := context.Param(attacker, uint16(0x1b+index)); got != float32(index*10) {
			t.Errorf("monster element %d: resistance %v, want %d", index, got, index*10)
		}
	}
}

/*
================
TestMovementAbnormalsPublishAndRestoreEffectiveSpeed

The live mover and the client packet must agree both with and without haste
and berserk. Expiry must restore the remaining modifiers exactly once.
================
*/
func TestMovementAbnormalsPublishAndRestoreEffectiveSpeed(t *testing.T) {
	for _, status := range []abnormal.Status{abnormal.Frostbite, abnormal.Slow} {
		for _, boosted := range []bool{false, true} {
			rt, clock, character, source := newCombatTestRuntime(t, 100)
			factor := float32(1)
			if boosted {
				character.NativeBodyStatus = 1
				rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: character.Name,
					SkillID: 1, SkillGroup: 1, InstanceToken: 1, State: statuseffect.StateActive,
					Movement: true, MovementPercent: 20})
				factor = 2.4
			}
			rt.refreshMovementEffects(testDivision, character, clock.NowMs())
			record := abnormal.Record{Status: status, Level: 1, Grade: 1, DurationMs: 1000, SourceGID: source.Gid}
			owner := rt.applyPlayerAbnormalInDoor(testDivision, character, false, []abnormal.Record{record}, clock.NowMs())
			publication := rt.playerAbnormalPublication(testDivision, character, owner)
			penalty := float32(0.75)
			if status == abnormal.Frostbite {
				penalty = 0.5
			}
			wantWalk, wantRun := float32(simulation.WalkSpeed)*factor*penalty, float32(simulation.RunSpeed)*factor*penalty
			walk, run := worldSpeeds(rt, character)
			if walk != wantWalk || run != wantRun {
				t.Errorf("status %d boosted %v: speeds %v/%v, want %v/%v", status, boosted, walk, run, wantWalk, wantRun)
			}
			published := false
			for _, frame := range publication.public {
				if frame.Opcode == 0x376f {
					published = len(frame.Payload) == 12 &&
						math.Float32frombits(binary.LittleEndian.Uint32(frame.Payload[4:])) == wantWalk &&
						math.Float32frombits(binary.LittleEndian.Uint32(frame.Payload[8:])) == wantRun
				}
			}
			if !published {
				t.Errorf("status %d boosted %v: missing effective speed packet", status, boosted)
			}
			rt.advancePlayerAbnormals(clock.NowMs() + 1001)
			if walk, run = worldSpeeds(rt, character); walk != float32(simulation.WalkSpeed)*factor || run != float32(simulation.RunSpeed)*factor {
				t.Errorf("status %d restored speeds %v/%v", status, walk, run)
			}
		}
	}
}

/*
================
TestImmobilizingStatusSettlesPlayerMovement

Rejecting the next move does not stop a segment already in flight. All four
immobilizers must settle the live pose and publish its source correction.
================
*/
func TestImmobilizingStatusSettlesPlayerMovement(t *testing.T) {
	for _, status := range []abnormal.Status{abnormal.Freeze, abnormal.Sleep, abnormal.Stun, abnormal.Root} {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		key := simulation.WorldKey(testDivision, character.Name)
		before := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(character) }, func(world *simulation.WorldState) {
			from := world.Spawn
			world.Spawn.X += 100
			world.MoveSegment = &simulation.MoveSegment{From: from, StartedAtMs: clock.NowMs() - 500, ArrivesAtMs: clock.NowMs() + 500}
		})
		want := before.LiveSpawnAt(clock.NowMs())
		record := abnormal.Record{Status: status, Level: 1, Grade: 1, DurationMs: 1000, SourceGID: source.Gid}
		owner := rt.applyPlayerAbnormalInDoor(testDivision, character, false, []abnormal.Record{record}, clock.NowMs())
		publication := rt.playerAbnormalPublication(testDivision, character, owner)
		after := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
		if after.MoveSegment != nil || after.Spawn != want || after.LiveSpawnAt(clock.NowMs()+5000) != want {
			t.Errorf("status %d retained movement: %+v; want %+v", status, after, want)
		}
		corrected := false
		for _, frame := range publication.public {
			if frame.Opcode == wire.OpObjectSourceCorrection {
				value, err := wire.DecodeObjectSourceCorrection(frame.Payload)
				corrected = err == nil && value.Gid == enterworld.ObjectIDForCharacter(character) && value.Position.X == float32(want.X)
			}
		}
		if !corrected {
			t.Errorf("status %d did not publish the stopped position", status)
		}
	}
}

/*
================
TestPotionRecoveryHonorsAbnormalReductions

All three recovery families pass through 4A86A0. Zombie reverses the full
authored HP amount before that recovery path; MP still receives its reduction.
================
*/
func TestPotionRecoveryHonorsAbnormalReductions(t *testing.T) {
	for _, kind := range []int64{1, 2, 3} {
		for _, zombie := range []bool{false, true} {
			character := testCharacter()
			character.CurrentHP, character.CurrentMP = testInt64(150), testInt64(30)
			items := testItems()
			ref := *items["ITEM_ETC_HP_POTION_01"]
			ref.TypeIDs[3] = kind
			ref.RecoveryHP, ref.RecoveryMP = 0, 0
			if kind != 2 {
				ref.RecoveryHP = 80
			}
			if kind != 1 {
				ref.RecoveryMP = 80
			}
			items[ref.Codename] = &ref
			character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
				Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2,
			})
			rt, clock := newTestRuntime(character, items)
			gid := enterworld.ObjectIDForCharacter(character)
			records := []abnormal.Record{
				{Status: abnormal.Panic, Grade: 1, DurationMs: 10000, SourceGID: gid, Param34: 50},
				{Status: abnormal.Combustion, Grade: 1, DurationMs: 10000, SourceGID: gid, Param34: 25},
			}
			if zombie {
				records = append(records, abnormal.Record{Status: abnormal.Zombie, Level: 1, DurationMs: 10000, SourceGID: gid})
			}
			rt.applyPlayerAbnormalInDoor(testDivision, character, false, records, clock.NowMs())
			stats, _, err := rt.playerCombatStats(testDivision, character)
			if err != nil {
				t.Fatal(err)
			}
			maxHP, maxMP, hp, mp := rt.playerKeeperVitals(testDivision, character)
			amount, ok := computePotionAmount(&ref, stats.Level, stats.Strength, stats.Intellect, maxHP, maxMP)
			if !ok {
				t.Fatal("invalid potion fixture")
			}
			wantHP, wantMP := hp, mp
			if !zombie {
				if amount.hp > 0 {
					amount.hp = max(amount.hp/5, 1)
				}
				if amount.mp > 0 {
					amount.mp = max(amount.mp/5, 1)
				}
			}
			if amount.hp > 0 {
				wantHP = min(maxHP, hp+amount.hp/2)
				if zombie {
					wantHP = max(0, hp-amount.hp)
				}
			}
			if amount.mp > 0 {
				wantMP = min(maxMP, mp+amount.mp*3/4)
			}
			request := wire.NewWriter(3).U8(21).U16(wire.PackTypeFlags(3, 3, 1, uint8(kind))).Payload()
			result := rt.HandleItemUse(testDivision, character, request)
			if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
				t.Fatalf("kind %d zombie %v: potion refused %+v", kind, zombie, result)
			}
			if enterworld.CurrentHP(character) != wantHP || *character.CurrentMP != wantMP {
				t.Errorf("kind %d zombie %v: HP/MP %d/%d, want %d/%d", kind, zombie,
					enterworld.CurrentHP(character), *character.CurrentMP, wantHP, wantMP)
			}
		}
	}
}

/*
================
TestMonsterMyopiaChangesPursuitAndBothVictimKinds
================
*/
func TestMonsterMyopiaChangesPursuitAndBothVictimKinds(t *testing.T) {
	for _, petTarget := range []bool{false, true} {
		rt, clock, c, m := newCombatTestRuntime(t, 100)
		skills := rt.deps.SkillData().(staticSkillSource)
		skill := skills[2]
		skill.ActionRange = 100
		skill.ActionCastingTimeMs = 0
		skills[2] = skill
		m.Ref.DefaultSkillIDs[0] = 2
		record := abnormal.Record{Status: abnormal.Myopia, Grade: 1, DurationMs: 10000, SourceGID: enterworld.ObjectIDForCharacter(c), SourceName: c.Name, Param3C: 90}
		records := []abnormal.Record{record}
		impacts := rt.Monsters.ApplyDamageSequence(testDivision, m.Gid, m.CurrentHP, []simulation.MonsterDamagePlan{{GID: m.Gid, Abnormal: records, AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, records)}})
		if len(impacts) != 1 {
			t.Fatal("myopia admission failed")
		}
		m.Abnormal = impacts[0].Instance.Abnormal
		plan, valid := rt.MonsterAttackPlan(m, 2, simulation.AttackPick{})
		if !valid || plan.Reach != 10 {
			t.Fatalf("myopic plan %+v", plan)
		}
		mover, _ := rt.Monsters.Mover(testDivision, m.Gid)
		pose := mover.LivePoseAt(clock.NowMs(), nil)
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(world *simulation.WorldState) {
			world.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X + 75, Y: pose.Y, Z: pose.Z}
		})
		var result simulation.MonsterAttackResult
		if petTarget {
			equipCombatTestPet(t, rt, c, 4)
			result = rt.monsterHitSummonedCOS(testDivision, m, monsterCastRecipient{c, c.ActiveCOS.GID}, 2, clock.NowMs(), nil)
		} else {
			result = rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
		}
		if result.Accepted || result.Refusal != simulation.MonsterAttackApproachRequired {
			t.Fatalf("pet %v bypassed myopia: %+v", petTarget, result)
		}
	}
}
