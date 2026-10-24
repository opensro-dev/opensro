/*
===========================================================================
petwolfhunt_test.go - shared marks and Rend bonus through real combat doors.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"testing"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestWolfBloodHuntMarkLifecycle
================
*/
func TestWolfBloodHuntMarkLifecycle(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, mob := wolfFieldFixture(t, 1)
			learnPetBook(t, c, 12+tier)
			step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
			rt.wolfExecuteAttack(testDivision, c, 0, 1, []uint32{mob.Gid, mob.Gid}, step.nowMs)
			want := uint8(0)
			if tier >= 2 {
				want = 1
			}
			if step.state.wolf.huntHits != want {
				t.Fatal("owner tier/duplicate hits", step.state.wolf)
			}
			for range 8 {
				rt.wolfExecuteAttack(testDivision, c, c.ActiveCOS.GID, 0, []uint32{mob.Gid, mob.Gid}, step.nowMs)
			}
			if step.state.wolf.huntHits != 4 {
				t.Fatal("mark did not cap at four")
			}
			f := petSkillCounterFrame(step.state, c.ActiveCOS.GID)
			if len(f.Payload) != 17 || f.Payload[12] != 4 || binary.LittleEndian.Uint32(f.Payload[13:]) != mob.Gid {
				t.Fatal("mark not replicated")
			}
			at := rt.LiveSpawnFor(testDivision, c)
			at.X += 40
			other := wolfSpawnAt(t, rt, c, at, step.nowMs)
			rt.wolfHuntAttack(step, 0, []uint32{other.Gid})
			if step.state.wolf.huntHits != 1 || step.state.wolf.huntTarget != other.Gid {
				t.Fatal("retarget retained mark")
			}
			rt.advanceWolfKit(step.key, step.nowMs+wolfHuntDurationMs)
			if step.state.wolf.huntHits != 0 || step.state.wolf.huntTarget != 0 {
				t.Fatal("expired mark")
			}
			rt.wolfHuntAttack(step, 0, []uint32{mob.Gid})
			c.ActiveCOS.Training.Slotted[2] = 0
			rt.advanceWolfKit(step.key, step.nowMs)
			if step.state.wolf.huntHits != 0 {
				t.Fatal("unslotted mark")
			}
			c.ActiveCOS.Training.Slotted[2] = tier
			rt.wolfHuntAttack(step, 0, []uint32{mob.Gid})
			rt.AttackPetExpansion = false
			rt.advanceWolfKit(step.key, step.nowMs)
			if step.state.wolf.huntHits != 0 {
				t.Fatal("flag-off retained mark")
			}
		})
	}
}

/*
================
TestWolfBloodHuntRealRendBonus

Drive public commands and simulation ticks, preserving authored ranks.
================
*/
func TestWolfBloodHuntRealRendBonus(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			damage := func(mark bool) (uint32, uint8) {
				rt, clock, c, mob := wolfFieldFixture(t, 1)
				learnPetBook(t, c, 3+tier)
				hunt := learnPetBook(t, c, 12+tier)
				step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
				if mark {
					for range 4 {
						rt.wolfHuntAttack(step, 0, []uint32{mob.Gid})
					}
				}
				rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 3+tier), mob.Gid))
				tickPetCombat(t, rt, clock, 100, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, 1, clock.NowMs()) })
				after, _ := rt.Monsters.Get(testDivision, mob.Gid)
				if companion.Level(c.ActiveCOS, hunt) != 1 {
					t.Fatal("rank changed")
				}
				return mob.CurrentHP - after.CurrentHP, step.state.wolf.huntHits
			}
			normal, _ := damage(false)
			boosted, charges := damage(true)
			expected := normal + uint32(float64(normal)*float64(50+25*(tier-1))/100)
			if boosted != expected || charges != 1 {
				t.Fatalf("Rend normal=%d boosted=%d expected=%d charges=%d", normal, boosted, expected, charges)
			}
		})
	}
}

/*
================
TestWolfBloodHuntRendGuardsAndBurst
================
*/
func TestWolfBloodHuntRendGuardsAndBurst(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	learnPetBook(t, c, 6)
	learnPetBook(t, c, 15)
	at := rt.LiveSpawnFor(testDivision, c)
	at.X += 35
	near := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 6), mob.Gid))
	tickPetCombat(t, rt, clock, 100, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, 1, clock.NowMs()) })
	rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(c.ActiveCOS.GID).U8(wire.CosCommandFollowTag).Payload())
	step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	for range 4 {
		rt.wolfHuntAttack(step, 0, []uint32{mob.Gid})
	}
	target, _ := rt.resolvePetCombatTarget(step, mob.Gid)
	step.trained = &petTrainedStrike{rend: true}
	hit := monsterStrike{impacts: []wire.SkillCastTargetImpact{{Blocked: true, Damage: 100}}}
	rt.wolfHuntRend(step, target, hit)
	if step.state.wolf.huntHits != 4 {
		t.Fatal("blocked Rend consumed mark")
	}
	hit.impacts[0].Blocked = false
	target.gid++
	rt.wolfHuntRend(step, target, hit)
	if step.state.wolf.huntHits != 4 {
		t.Fatal("other prey consumed mark")
	}
	target.gid--
	before, _ := rt.Monsters.Get(testDivision, near.Gid)
	if !petTargetBleeds(before) {
		t.Fatal("real Rend did not bleed neighbour")
	}
	at.X++
	clean := wolfSpawnAt(t, rt, c, at, step.nowMs)
	rt.wolfHuntRend(step, target, hit)
	after, _ := rt.Monsters.Get(testDivision, near.Gid)
	untouched, _ := rt.Monsters.Get(testDivision, clean.Gid)
	if before.CurrentHP-after.CurrentHP != 50 || untouched.CurrentHP != clean.CurrentHP || step.state.wolf.huntHits != 0 {
		t.Fatal("burst filtering/consumption", before.CurrentHP, after.CurrentHP)
	}
	rt.wolfHuntRend(step, target, hit)
	twice, _ := rt.Monsters.Get(testDivision, near.Gid)
	if twice.CurrentHP != after.CurrentHP {
		t.Fatal("mark consumed twice")
	}
}

/*
================
TestWolfBloodHuntBurstKillCreditsNoQuest

The Blood Hunt burst that kills a bleeding neighbour is a companion kill:
the burst goes through its real caller (wolfHuntRend -> wolfBonusHit) and
no quest kill is credited, though the neighbour dies.
================
*/
func TestWolfBloodHuntBurstKillCreditsNoQuest(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	learnPetBook(t, c, 6)
	learnPetBook(t, c, 15)
	quested := 0
	rt.UpdateQuestKill = func(*enterworld.Character, string, uint8) ([]wire.Frame, bool) {
		quested++
		return nil, true
	}
	at := rt.LiveSpawnFor(testDivision, c)
	at.X += 35
	near := wolfSpawnAt(t, rt, c, at, clock.NowMs())
	rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 6), mob.Gid))
	tickPetCombat(t, rt, clock, 100, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, 1, clock.NowMs()) })
	rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(c.ActiveCOS.GID).U8(wire.CosCommandFollowTag).Payload())
	step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	for range 4 {
		rt.wolfHuntAttack(step, 0, []uint32{mob.Gid})
	}
	target, _ := rt.resolvePetCombatTarget(step, mob.Gid)
	step.trained = &petTrainedStrike{rend: true}
	hit := monsterStrike{impacts: []wire.SkillCastTargetImpact{{Damage: 100}}}
	bleeding, _ := rt.Monsters.Get(testDivision, near.Gid)
	if !petTargetBleeds(bleeding) {
		t.Fatal("real Rend did not bleed the neighbour")
	}
	quested = 0
	if _, ok := rt.Monsters.ApplyDamage(testDivision, near.Gid, bleeding.CurrentHP-1); !ok {
		t.Fatal("could not wound the neighbour")
	}
	rt.wolfHuntRend(step, target, hit)
	after, _ := rt.Monsters.Get(testDivision, near.Gid)
	if after.CurrentHP != 0 {
		t.Fatalf("the burst left the neighbour at %d HP", after.CurrentHP)
	}
	if quested != 0 {
		t.Fatalf("the Blood Hunt burst kill credited %d quest kills, want 0", quested)
	}
}
