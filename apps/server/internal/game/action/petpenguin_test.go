/*
===========================================================================
petpenguin_test.go - frost books through public commands and simulation ticks.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
penguinFixture
================
*/
func penguinFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_PENGUIN_040", 90)
	rt.AttackPetExpansion = true
	mob := roarField(t, rt, c, clock)
	return rt, clock, c, mob
}

/*
================
castPenguinFixture
================
*/
func castPenguinFixture(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, mob monster.Instance, id uint8) {
	t.Helper()
	row := int((id - 31) / 3)
	rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, id), mob.Gid))
	tickPetCombat(t, rt, clock, 200, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, row, clock.NowMs()) })
	rt.HandleCosCommand(testDivision, c, append(binary.LittleEndian.AppendUint32(nil, c.ActiveCOS.GID), 9))
}

/*
================
TestPenguinLanceAndHourglassBooks
================
*/
func TestPenguinLanceAndHourglassBooks(t *testing.T) {
	for book := uint8(1); book <= 3; book++ {
		t.Run(fmt.Sprint(book), func(t *testing.T) {
			rt, clock, c, mob := penguinFixture(t)
			learnPetBook(t, c, 30+book)
			learnPetBook(t, c, 42+book)
			step, ok := rt.penguinCompanion(testDivision, c, clock.NowMs())
			if !ok {
				t.Fatal("fixture eligibility")
			}
			step.state.penguin.hourglass = 5
			castPenguinFixture(t, rt, clock, c, mob, 30+book)
			after, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if after.CurrentHP >= mob.CurrentHP {
				t.Fatal("ranged spell did not deal damage")
			}
			left := c.ActiveCOS.Training.Rows[0].ReadyAtMs - clock.NowMs()
			if left != 4000 {
				t.Fatal("shared cooldown reduction", left)
			}
			if step.state.penguin.hourglass != 1 {
				t.Fatal("spent charges did not start one new action", step.state.penguin.hourglass)
			}
			if book == 3 && after.Abnormal != nil && after.Abnormal.Has(abnormal.Frostbite) {
				slot := after.Abnormal.Slots[abnormal.Frostbite]
				if slot.DurationMs < 2000 {
					t.Fatal("extended frostbite")
				}
			}
		})
	}
}

/*
================
TestPenguinStormPulsesAndFinalFreeze
================
*/
func TestPenguinStormPulsesAndFinalFreeze(t *testing.T) {
	for book := uint8(1); book <= 3; book++ {
		t.Run(fmt.Sprint(book), func(t *testing.T) {
			rt, clock, c, mob := penguinFixture(t)
			learnPetBook(t, c, 33+book)
			learnPetBook(t, c, 42+book)
			castPenguinFixture(t, rt, clock, c, mob, 33+book)
			step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
			first, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if len(step.state.penguin.storms) != 1 {
				t.Fatal("storm not armed")
			}
			last := first.CurrentHP
			for range 3 {
				clock.now = clock.now.Add(time.Second)
				batches := rt.TickHook()(clock.NowMs())
				next, _ := rt.Monsters.Get(testDivision, mob.Gid)
				if next.CurrentHP >= last {
					t.Fatal("missing storm pulse", book, next.CurrentHP, last)
				}
				ownerHP := uint32(0)
				ownerDamage := uint32(0)
				ownerFrozen := false
				for _, batch := range batches {
					if batch.OnlyCharacterID != c.ID {
						continue
					}
					for _, frame := range batch.Frames {
						if frame.Opcode == abnormalDamageCreditOpcode && len(frame.Payload) == abnormalDamageCreditBytes && binary.LittleEndian.Uint32(frame.Payload) == mob.Gid {
							ownerDamage += binary.LittleEndian.Uint32(frame.Payload[4:])
						}
						if frame.Opcode != simulation.OpVitalsUpdate || len(frame.Payload) < 11 || binary.LittleEndian.Uint32(frame.Payload) != mob.Gid {
							continue
						}
						if frame.Payload[6]&1 != 0 {
							ownerHP = binary.LittleEndian.Uint32(frame.Payload[7:])
						}
						if frame.Payload[6]&4 != 0 {
							offset := 7
							if frame.Payload[6]&1 != 0 {
								offset += 4
							}
							if frame.Payload[6]&2 != 0 {
								offset += 4
							}
							ownerFrozen = binary.LittleEndian.Uint32(frame.Payload[offset:])&1 != 0
						}
					}
				}
				if ownerHP != next.CurrentHP {
					t.Fatal("storm HP update missing for owner", ownerHP, next.CurrentHP)
				}
				if ownerDamage != last-next.CurrentHP {
					t.Fatal("storm damage number missing for owner", ownerDamage, last-next.CurrentHP)
				}
				if next.Abnormal != nil && next.Abnormal.Has(abnormal.Freeze) && !ownerFrozen {
					t.Fatal("storm freeze update missing for owner")
				}
				last = next.CurrentHP
			}
			end, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if len(step.state.penguin.storms) != 0 || step.state.penguin.hourglass != 1 {
				t.Fatal("pulses fed counters or survived expiry")
			}
			frozen := end.Abnormal != nil && end.Abnormal.Has(abnormal.Freeze)
			if frozen != (book == 3) {
				t.Fatal("end freeze book gate", book, frozen)
			}
			if c.ActiveCOS.Training.Rows[1].ReadyAtMs-clock.NowMs() != 17000 {
				t.Fatal("pulses refreshed cooldown")
			}
		})
	}
}

/*
================
TestPenguinOwnerPoolsExpiryAndFlagOff
================
*/
func TestPenguinOwnerPoolsExpiryAndFlagOff(t *testing.T) {
	for book := uint8(1); book <= 3; book++ {
		t.Run(fmt.Sprint(book), func(t *testing.T) {
			rt, clock, c, mob := penguinFixture(t)
			learnPetBook(t, c, 39+book)
			learnPetBook(t, c, 42+book)
			step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
			magic := enterworld.SkillRow{Attack: enterworld.SkillAttack{Flags: 8}}
			for range 8 {
				rt.penguinOwnerHit(testDivision, c, magic, []monster.Instance{mob}, []combat.Result{{Damage: 1}, {Damage: 1}}, clock.NowMs())
			}
			want := uint8(0)
			if book >= 2 {
				want = 5
			}
			if step.state.penguin.stacks[1] != want || step.state.penguin.hourglass != want {
				t.Fatal("owner book gate/action cap", step.state.penguin)
			}
			if (len(rt.penguinMagicWrites(testDivision, c)) > 0) != (book >= 2) {
				t.Fatal("owner stat projection")
			}
			clock.now = clock.now.Add(9 * time.Second)
			rt.advancePenguin(step.key, clock.NowMs())
			if len(rt.penguinMagicWrites(testDivision, c)) != 0 || step.state.penguin.stacks[1] != 0 {
				t.Fatal("expired stats remain")
			}
			rt.penguinProgress(step, 0)
			rt.AttackPetExpansion = false
			rt.advancePenguin(step.key, clock.NowMs())
			if c.ActiveCOS.ArcaneMagicPercent != 0 || step.state.penguin.hourglass != 0 {
				t.Fatal("flag off leaked penguin state")
			}
		})
	}
}

/*
================
TestPenguinEchoIsOneExtraCastWithoutExtraCooldown
================
*/
func TestPenguinEchoIsOneExtraCastWithoutExtraCooldown(t *testing.T) {
	rt, clock, c, mob := penguinFixture(t)
	learnPetBook(t, c, 33)
	learnPetBook(t, c, 42)
	step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
	step.state.penguin.stacks[0] = 5
	step.state.penguin.until[0] = clock.NowMs() + 8000
	rt.publishPenguinMagic(step)
	rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 33), mob.Gid))
	frames := tickPetCombat(t, rt, clock, 200, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, 0, clock.NowMs()) })
	var windup, echo uint32
	for _, frame := range frames {
		if frame.Opcode != 0xb245 || len(frame.Payload) < 19 || binary.LittleEndian.Uint32(frame.Payload[6:]) != c.ActiveCOS.GID {
			continue
		}
		if frame.Payload[18] == 0 {
			windup = binary.LittleEndian.Uint32(frame.Payload[10:])
		} else {
			echo = binary.LittleEndian.Uint32(frame.Payload[10:])
		}
	}
	if windup == 0 || echo == 0 || windup == echo {
		t.Fatal("echo must publish its own cast identity", windup, echo)
	}
	if step.state.penguin.stacks[0] > 1 || c.ActiveCOS.Training.Rows[0].ReadyAtMs-clock.NowMs() != 6000 {
		t.Fatal("echo stacked or refreshed its row")
	}
}

/*
================
TestPenguinShatterOwnerGateAndFrozenKillBurst
================
*/
func TestPenguinShatterOwnerGateAndFrozenKillBurst(t *testing.T) {
	for book := uint8(1); book <= 3; book++ {
		t.Run(fmt.Sprint(book), func(t *testing.T) {
			rt, clock, c, mob := penguinFixture(t)
			learnPetBook(t, c, 30+book)
			learnPetBook(t, c, 36+book)
			castPenguinFixture(t, rt, clock, c, mob, 30+book)
			chilled, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if !petTargetChilled(chilled) {
				t.Fatal("fixture did not frostbite")
			}
			hits := []combat.Result{{Damage: 100}}
			rt.penguinOwnerShatter(testDivision, c, chilled, hits, clock.NowMs())
			want := uint32(100)
			if book >= 2 {
				want = 110
			}
			if hits[0].Damage != want {
				t.Fatal("owner Shatter tier gate", hits)
			}
		})
	}
	rt, clock, c, mob := penguinFixture(t)
	learnPetBook(t, c, 36)
	learnPetBook(t, c, 39)
	castPenguinFixture(t, rt, clock, c, mob, 36)
	for range 3 {
		clock.now = clock.now.Add(time.Second)
		rt.TickHook()(clock.NowMs())
	}
	frozen, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if frozen.Abnormal == nil || !frozen.Abnormal.Has(abnormal.Freeze) {
		t.Fatal("native freeze missing")
	}
	step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
	at := step.state.follower.Position(clock.NowMs())
	at.X += 35
	near := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	wolfWeaken(t, rt, frozen, 1)
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, mob.Gid))
	tickPetCombat(t, rt, clock, 150, func() bool { dead, _ := rt.Monsters.Get(testDivision, mob.Gid); return dead.CurrentHP == 0 })
	after, _ := rt.Monsters.Get(testDivision, near.Gid)
	if after.CurrentHP >= near.CurrentHP {
		t.Fatal("frozen kill did not damage nearby monster")
	}
}

/*
================
TestPenguinEchoAddsActualDamage
================
*/
func TestPenguinEchoAddsActualDamage(t *testing.T) {
	damage := func(stacks uint8) uint32 {
		rt, clock, c, mob := penguinFixture(t)
		learnPetBook(t, c, 33)
		learnPetBook(t, c, 42)
		step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
		step.state.penguin.stacks[0] = stacks
		step.state.penguin.until[0] = clock.NowMs() + 8000
		rt.publishPenguinMagic(step)
		castPenguinFixture(t, rt, clock, c, mob, 33)
		after, _ := rt.Monsters.Get(testDivision, mob.Gid)
		return mob.CurrentHP - after.CurrentHP
	}
	one, two := damage(4), damage(5)
	if one == 0 || two < one*3/2 {
		t.Fatal("charged active did not echo", one, two)
	}
}

/*
================
TestPenguinLancePiercesForwardOnly
================
*/
func TestPenguinLancePiercesForwardOnly(t *testing.T) {
	rt, clock, c, primary := penguinFixture(t)
	learnPetBook(t, c, 32)
	step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
	from := step.state.follower.Position(clock.NowMs())
	ahead := from
	ahead.X += 95
	second := wolfSpawnAt(t, rt, c, ahead, clock.NowMs())
	behind := from
	behind.X -= 70
	back := wolfSpawnAt(t, rt, c, behind, clock.NowMs())
	offLine := from
	offLine.Z += 100
	side := wolfSpawnAt(t, rt, c, offLine, clock.NowMs())
	castPenguinFixture(t, rt, clock, c, primary, 32)
	for _, want := range []struct {
		mob monster.Instance
		hit bool
	}{{second, true}, {back, false}, {side, false}} {
		live, _ := rt.Monsters.Get(testDivision, want.mob.Gid)
		if (live.CurrentHP < want.mob.CurrentHP) != want.hit {
			t.Fatal("piercing geometry", want.mob.Gid, want.hit, live.CurrentHP, want.mob.CurrentHP)
		}
	}
}

/*
================
TestPenguinHourglassExtendsNativeFrostbite
================
*/
func TestPenguinHourglassExtendsNativeFrostbite(t *testing.T) {
	duration := func(charges uint8) uint32 {
		rt, clock, c, mob := penguinFixture(t)
		learnPetBook(t, c, 33)
		learnPetBook(t, c, 45)
		step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
		step.state.penguin.hourglass = charges
		castPenguinFixture(t, rt, clock, c, mob, 33)
		live, _ := rt.Monsters.Get(testDivision, mob.Gid)
		if live.Abnormal == nil || !live.Abnormal.Has(abnormal.Frostbite) {
			t.Fatal("missing frostbite")
		}
		return live.Abnormal.Slots[abnormal.Frostbite].DurationMs
	}
	plain, extended := duration(0), duration(5)
	if extended != plain+2000 {
		t.Fatal("Hourglass duration", plain, extended)
	}
}

/*
================
TestPenguinStormNeedsContinuousExposureAndCancelsOnDeath
================
*/
func TestPenguinStormNeedsContinuousExposureAndCancelsOnDeath(t *testing.T) {
	rt, clock, c, mob := penguinFixture(t)
	learnPetBook(t, c, 36)
	castPenguinFixture(t, rt, clock, c, mob, 36)
	rt.LineOfSight = func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool { return false }
	clock.now = clock.now.Add(time.Second)
	rt.TickHook()(clock.NowMs())
	rt.LineOfSight = nil
	for range 2 {
		clock.now = clock.now.Add(time.Second)
		rt.TickHook()(clock.NowMs())
	}
	live, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if live.Abnormal != nil && live.Abnormal.Has(abnormal.Freeze) {
		t.Fatal("storm froze a target that missed a pulse behind cover")
	}
	c.ActiveCOS.Training.Rows[1].ReadyAtMs = 0
	castPenguinFixture(t, rt, clock, c, live, 36)
	step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
	if len(step.state.penguin.storms) != 1 {
		t.Fatal("second storm missing")
	}
	c.ActiveCOS.CurrentHP = 0
	clock.now = clock.now.Add(time.Second)
	rt.advancePenguin(step.key, clock.NowMs())
	if len(step.state.penguin.storms) != 0 || step.state.penguin.hourglass != 0 {
		t.Fatal("dead pet retained frost combat")
	}
}

/*
================
TestPenguinBlizzardEchoDoesNotFeedCounters
================
*/
func TestPenguinBlizzardEchoDoesNotFeedCounters(t *testing.T) {
	rt, clock, c, mob := penguinFixture(t)
	learnPetBook(t, c, 36)
	learnPetBook(t, c, 42)
	learnPetBook(t, c, 45)
	step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
	step.state.penguin.stacks[0] = 5
	step.state.penguin.until[0] = clock.NowMs() + 8000
	castPenguinFixture(t, rt, clock, c, mob, 36)
	if len(step.state.penguin.storms) != 2 {
		t.Fatal("Blizzard echo missing")
	}
	for range 3 {
		clock.now = clock.now.Add(time.Second)
		rt.TickHook()(clock.NowMs())
	}
	if step.state.penguin.stacks[0] != 1 || step.state.penguin.hourglass != 1 {
		t.Fatal("echo pulses fed progress", step.state.penguin)
	}
}

/*
================
TestPenguinLegacyQueueCannotBypassTownGate
================
*/
func TestPenguinLegacyQueueCannotBypassTownGate(t *testing.T) {
	rt, clock, c, mob := penguinFixture(t)
	learnPetBook(t, c, 31)
	step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
	rt.HandleCosCommand(testDivision, c, trainingCommand(c.ActiveCOS.GID, petTrainingUse, 31))
	c.World.Spawn.RegionID = testInt64(23687)
	rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
	if !rt.inTown(testDivision, c) {
		t.Fatal("fixture not in town")
	}
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Flags: 4, Min: 100, Max: 200, Percent: 100}}
	got, plan := rt.petTrainedDecide(step, skill, petCombatTarget{monster: &mob})
	if plan != nil || got.Attack != skill.Attack {
		t.Fatal("legacy queued spell gained expansion bonuses in town")
	}
}

/*
================
TestPenguinEchoFrozenKillStillShattersWithoutProgress
================
*/
func TestPenguinEchoFrozenKillStillShattersWithoutProgress(t *testing.T) {
	rt, clock, c, mob := penguinFixture(t)
	learnPetBook(t, c, 36)
	learnPetBook(t, c, 39)
	castPenguinFixture(t, rt, clock, c, mob, 36)
	for range 3 {
		clock.now = clock.now.Add(time.Second)
		rt.TickHook()(clock.NowMs())
	}
	step, _ := rt.penguinCompanion(testDivision, c, clock.NowMs())
	at := step.state.follower.Position(clock.NowMs())
	at.X += 35
	near := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	frozen, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if frozen.Abnormal == nil || !frozen.Abnormal.Has(abnormal.Freeze) {
		t.Fatal("fixture did not freeze")
	}
	wolfWeaken(t, rt, frozen, 1)
	target, _ := rt.resolvePetCombatTarget(step, mob.Gid)
	skill, _ := rt.deps.SkillData().SkillByID(step.ref.Parameters.DefaultSkillIDs[0])
	step.trained = &petTrainedStrike{mods: companion.ModifiersFor(step.pet), codename: step.pet.Codename,
		reserved: true, passive: -1, penguinRepeat: true}
	if _, ok := rt.strikePetTargets(step, target, skill); !ok {
		t.Fatal("echo strike refused")
	}
	after, _ := rt.Monsters.Get(testDivision, near.Gid)
	if after.CurrentHP >= near.CurrentHP {
		t.Fatal("echo killed a frozen enemy without Shatter")
	}
	if step.state.penguin.stacks != [2]uint8{} || step.state.penguin.hourglass != 0 {
		t.Fatal("echo fed counters")
	}
}

/*
================
TestPenguinBlizzardPublishesOneField
================
*/
func TestPenguinBlizzardPublishesOneField(t *testing.T) {
	rt, clock, c, mob := penguinFixture(t)
	learnPetBook(t, c, 36)
	at := simulation.Spawn{RegionID: mob.Spawn.RegionID, X: mob.Spawn.X + 10, Y: mob.Spawn.Y, Z: mob.Spawn.Z}
	wolfSpawnAt(t, rt, c, at, clock.NowMs())
	rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 36), mob.Gid))
	frames := tickPetCombat(t, rt, clock, 200, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, 1, clock.NowMs()) })
	clock.now = clock.now.Add(100 * time.Millisecond)
	for _, batch := range rt.TickHook()(clock.NowMs()) {
		frames = append(frames, batch.Frames...)
	}
	found := false
	for _, frame := range frames {
		if frame.Opcode != wire.OpActionEmote || len(frame.Payload) != 5 || frame.Payload[4] != 0xd5 {
			continue
		}
		if binary.LittleEndian.Uint32(frame.Payload) != mob.Gid {
			t.Fatal("secondary victim created a duplicate Blizzard field")
		}
		found = true
	}
	if !found {
		t.Fatal("Blizzard field signal missing")
	}
}
