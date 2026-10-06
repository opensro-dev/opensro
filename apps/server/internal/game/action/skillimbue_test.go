/*
===========================================================================

skillimbue_test.go - tests for skillimbue.go

===========================================================================
*/

package action

import (
	"encoding/binary"
	"errors"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

// burnSlot is the target's burn slot (4A46F0); the zero slot when none.
/*
================
burnSlot
================
*/
func burnSlot(m monster.Instance) abnormal.Slot {
	if m.Abnormal == nil {
		return abnormal.Slot{}
	}
	return m.Abnormal.Slots[abnormal.Burn]
}

/*
================
installFireImbue
================
*/
func installFireImbue(t *testing.T, rt *Runtime, c *enterworld.Character) enterworld.SkillRow {
	t.Helper()
	row := shippedOffense(t, "SKILL_CH_FIRE_GIGONGTA_A_01")
	if !row.Imbue.Pinned {
		t.Fatal("shipped Fire Force not admitted", row)
	}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(1000)
	return row
}

/*
================
TestWeaponImbueProductionActivationDamageAndExpiry
================
*/
func TestWeaponImbueProductionActivationDamageAndExpiry(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	before := rt.characterSnapshot(testDivision, c)
	writes := 0
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(c *enterworld.Character, reason string, update func() bool) bool {
		if reason == "skill-weapon-imbue" {
			writes++
		}
		return update()
	}
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	assertOpcodes(t, result.Frames, wire.OpSkillCastResult, simulation.OpVitalsUpdate, wire.OpSkillEffectControl, wire.OpSkillEffectControl, wire.OpAttachedEffect)
	if writes != 1 || *c.CurrentMP != 148 || *before.CurrentMP != 1000 || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("activation did not cross mutation boundary exactly once", writes, *c.CurrentMP, *before.CurrentMP, len(rt.effects.Snapshot(testDivision, c.Name)))
	}
	if rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("instant imbue blocks attacks for effect duration")
	}
	old := rt.effects.Snapshot(testDivision, c.Name)
	repeated := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if len(repeated.Frames) != 1 || repeated.Frames[0].Payload[1] != 5 || *c.CurrentMP != 148 {
		t.Fatal("repeat bypassed cooldown")
	}
	plain, _, plainC, plainTarget := newCombatTestRuntime(t, 100000)
	hit := plain.HandleTargetInteract(testDivision, plainC, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: plainTarget.Gid}.Encode())
	_, baseDamage, _ := assertSkillDamageOpen(t, hit.Frames, 2, enterworld.ObjectIDForCharacter(plainC), plainTarget.Gid)
	hit = rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	_, imbuedDamage, _ := assertSkillDamageOpen(t, hit.Frames, 2, enterworld.ObjectIDForCharacter(c), target.Gid)
	if imbuedDamage <= baseDamage || *c.CurrentMP != 148 {
		t.Fatal("imbue damage missing or MP charged again", baseDamage, imbuedDamage)
	}
	rt.TickHook()(clock.At(5*time.Second).UnixMilli() + 1)
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, clock.At(5*time.Second).UnixMilli()+1); imbue.Pinned || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || old[0].StopRequested {
		t.Fatal("expiry/snapshot ownership")
	}
}

/*
================
TestWeaponImbueRefusalsAndCancelReplacement
================
*/
func TestWeaponImbueRefusalsAndCancelReplacement(t *testing.T) {
	for _, mode := range []string{"unlearned", "dead", "mp", "target", "cancel", "disconnect", "death"} {
		t.Run(mode, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			row := installFireImbue(t, rt, c)
			request := wire.SkillAction{ActionId: row.ID}
			switch mode {
			case "unlearned":
				c.Skills = []uint32{2}
			case "dead":
				c.CurrentHP = testInt64(0)
			case "mp":
				c.CurrentMP = testInt64(51)
			case "target":
				request.HasTarget = true
				request.TargetGid = target.Gid
			}
			mp := *c.CurrentMP
			result := rt.HandleTargetInteract(testDivision, c, request.Encode())
			if mode != "cancel" && mode != "disconnect" && mode != "death" {
				if *c.CurrentMP != mp || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
					t.Fatal("refusal changed state", result)
				}
				return
			}
			token := rt.effects.Snapshot(testDivision, c.Name)[0].InstanceToken
			switch mode {
			case "cancel":
				rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: row.ID, InstanceToken: token}.Encode())
				if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, clock.NowMs()); imbue.Pinned {
					t.Fatal("stopped imbue still affects damage")
				}
				rt.TickHook()(clock.NowMs() + 1)
			case "disconnect":
				rt.ForgetCharacter(testDivision, c.Name)
			case "death":
				enemy := target
				enemy.Ref.DefaultSkillIDs[0] = 2
				enemy.Nest.NativeTacticsFlags = 0x200
				attack := rt.deps.SkillData().(staticSkillSource)[2]
				attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 100, 100, 100
				rt.deps.SkillData().(staticSkillSource)[2] = attack
				c.CurrentHP = testInt64(1)
				if hit := rt.MonsterBasicAttack(testDivision, enemy, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs()); !hit.Accepted || hit.TargetAlive {
					t.Fatal("fatal monster attack missing", hit)
				}
			}
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
				t.Fatal("retired imbue survived", mode)
			}
			// Re-entry/rebirth never gets the old effect back; a later legitimate cast
			// has a fresh token and cannot be retired by an earlier stop/expiry.
			c.CurrentHP = testInt64(100)
			clock.now = clock.At(6 * time.Second)
			rt.HandleTargetInteract(testDivision, c, request.Encode())
			current := rt.effects.Snapshot(testDivision, c.Name)
			if len(current) != 1 || current[0].InstanceToken == token {
				t.Fatal("new lifecycle failed")
			}
			rt.TickHook()(clock.NowMs() + 1)
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
				t.Fatal("stale retirement cleared new imbue")
			}
		})
	}
}

/*
================
TestImbueBurnProductionTickAndNonRefreshingReproc
================
*/
func TestImbueBurnProductionTickAndNonRefreshingReproc(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	hit := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(hit.Frames) < 2 {
		t.Fatal("burn status not published")
	}
	burned, _ := rt.Monsters.Get(testDivision, target.Gid)
	first := burnSlot(burned)
	if !first.Active || first.Level != 30 || first.Rate24 != 8 || first.DurationMs != 30*750 {
		t.Fatal("burn argument roles", first)
	}
	rt.TickHook()(clock.NowMs() + 1)
	ticked, _ := rt.Monsters.Get(testDivision, target.Gid)
	tick := burned.CurrentHP - ticked.CurrentHP
	if tick == 0 {
		t.Fatal("first native update did not tick burn")
	}
	rt.TickHook()(clock.NowMs() + 2001)
	same, _ := rt.Monsters.Get(testDivision, target.Gid)
	if same.CurrentHP != ticked.CurrentHP {
		t.Fatal("strict 2000-ms boundary lost")
	}
	rt.TickHook()(clock.NowMs() + 2002)
	next, _ := rt.Monsters.Get(testDivision, target.Gid)
	if next.CurrentHP != same.CurrentHP-tick {
		t.Fatal("periodic burn missing")
	}
	clock.now = clock.At(3 * time.Second)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	same, _ = rt.Monsters.Get(testDivision, target.Gid)
	if burnSlot(same).StartedAt != first.StartedAt {
		t.Fatal("equal burn refreshed duration")
	}
	rt.TickHook()(first.StartedAt + 22501)
	ended, _ := rt.Monsters.Get(testDivision, target.Gid)
	if burnSlot(ended).Active {
		t.Fatal("burn outlived its own duration")
	}
}

/*
================
TestImbueFormulaFailureDoesNotCommitBurnOrSpend
================
*/
func TestImbueFormulaFailureDoesNotCommitBurnOrSpend(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	rt.CombatRoll = func() (uint32, error) { return 0, errors.New("rng unavailable") }
	mp := *c.CurrentMP
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP != target.CurrentHP || burnSlot(after).Active || *c.CurrentMP != mp {
		t.Fatal("failed plan mutated authority")
	}
}

/*
================
TestSmashTransfersAfterCloseWithoutSpendingAgain
================
*/
func TestSmashTransfersAfterCloseWithoutSpendingAgain(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(19)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	releasePreparedSkillForTest(t, rt, clock.NowMs()+int64(skill.ActionCastingTimeMs)+1)
	life, _ := skill.ActionLifecycleMs()
	life++
	rt.TickHook()(clock.NowMs() + int64(life))
	if rt.castTokenCounter != 1 {
		t.Fatal("basic attack overtook close")
	}
	batches := rt.TickHook()(clock.NowMs() + int64(life) + 1)
	found := false
	for _, b := range batches {
		for _, f := range b.Frames {
			if f.Opcode == wire.OpSkillCastResult && len(f.Payload) > 6 && f.Payload[0] == 1 {
				if binary.LittleEndian.Uint32(f.Payload[2:]) != 2 {
					t.Fatal("repeated advanced skill")
				}
				found = true
			}
		}
	}
	if !found || *c.CurrentMP != 0 {
		t.Fatal("basic continuation missing or recharged skill")
	}
	rt.HandleTargetInteract(testDivision, c, []byte{2})
	rt.TickHook()(clock.NowMs() + 10000)
	if len(rt.combatIntentSnapshot()) != 0 || rt.castTokenCounter != 2 {
		t.Fatal("cancel resurrected continuation")
	}
}

/*
================
TestImbueAdmissionRechecksAuthorityAndCategory
================
*/
func TestImbueAdmissionRechecksAuthorityAndCategory(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(c *enterworld.Character, reason string, update func() bool) bool {
		if reason == "skill-weapon-imbue" {
			c.Skills = []uint32{2}
		}
		return update()
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || *c.CurrentMP != 1000 {
		t.Fatal("stale learned snapshot admitted")
	}
	rt.deps.(*enterworld.Deps).UpdateCharacter = nil
	c.Skills = append(c.Skills, row.ID)
	accepted := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	openToken := binary.LittleEndian.Uint32(accepted.Frames[0].Payload[10:])
	effect := rt.effects.Snapshot(testDivision, c.Name)[0]
	if openToken == effect.InstanceToken {
		t.Fatal("lasting descriptor borrowed closed action identity")
	}
	mp := *c.CurrentMP
	clock.now = clock.At(5 * time.Second)
	rt.TickHook()(clock.NowMs())
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, clock.NowMs()); !imbue.Pinned {
		t.Fatal("strict native expiry")
	}
	replaced := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	rows := rt.effects.Snapshot(testDivision, c.Name)
	if len(rows) != 1 || rows[0].StopRequested || rows[0].InstanceToken == effect.InstanceToken || *c.CurrentMP >= mp || len(replaced.Frames) == 0 {
		t.Fatal("equal-rank native replacement missing", replaced, rows)
	}
	if _, ok := findFrame(replaced.Frames, wire.OpEndedEffectInstances); !ok {
		t.Fatal("replacement did not publish the old imbue retirement")
	}
	rt.drainStoppedCharacterEffects()
	rows = rt.effects.Snapshot(testDivision, c.Name)
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, clock.NowMs()); len(rows) != 1 || rows[0].InstanceToken == effect.InstanceToken || !imbue.Pinned {
		t.Fatal("old retirement removed new imbue", rows)
	}
}

/*
================
TestBurnProductionSourceDepartureAndFatalPublication
================
*/
func TestBurnProductionSourceDepartureAndFatalPublication(t *testing.T) {
	for _, departed := range []bool{false, true} {
		t.Run(map[bool]string{false: "credited", true: "departed"}[departed], func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			row := installFireImbue(t, rt, c)
			rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
			current, _ := rt.Monsters.Get(testDivision, target.Gid)
			if !burnSlot(current).Active {
				t.Fatal("producer did not burn")
			}
			rt.Monsters.ApplyDamage(testDivision, target.Gid, current.CurrentHP-1)
			if departed {
				rt.ForgetCharacter(testDivision, c.Name)
			}
			batches := rt.TickHook()(clock.NowMs() + 1)
			privateDamage, zeroHP := 0, 0
			for _, b := range batches {
				for _, f := range b.Frames {
					if f.Opcode == 0x3128 {
						privateDamage++
						if b.OnlyCharacterID != c.ID {
							t.Fatal("private damage leaked")
						}
					}
					if f.Opcode == simulation.OpVitalsUpdate && len(f.Payload) >= 11 && f.Payload[6]&1 != 0 && binary.LittleEndian.Uint32(f.Payload) == target.Gid && binary.LittleEndian.Uint32(f.Payload[7:]) == 0 {
						zeroHP++
						if binary.LittleEndian.Uint16(f.Payload[4:]) != 2 {
							t.Fatal("wrong abnormal damage cause")
						}
					}
				}
			}
			if zeroHP != 1 || privateDamage != map[bool]int{false: 1, true: 0}[departed] {
				t.Fatal("fatal burn publication", zeroHP, privateDamage)
			}
			current, _ = rt.Monsters.Get(testDivision, target.Gid)
			if current.CurrentHP != 0 || burnSlot(current).Active {
				t.Fatal("fatal burn not committed")
			}
			if next := rt.advanceMonsterAbnormals(clock.NowMs() + 3000); len(next) != 0 {
				t.Fatal("burn killed twice")
			}
		})
	}
}

/*
================
TestImbueProjectileConsumesReleaseTimeEffect
================
*/
func TestImbueProjectileConsumesReleaseTimeEffect(t *testing.T) {
	for _, mode := range []string{"active", "cancelled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			rt, c, target, arrow, now := arrowFixture(t)
			fire := installFireImbue(t, rt, c)
			rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: fire.ID}.Encode())
			start, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: arrow.ID, HasTarget: true, TargetGid: target}, now)
			if decision != skillCastAccepted {
				t.Fatal(start)
			}
			after, _ := rt.Monsters.Get(testDivision, target)
			if burnSlot(after).Active {
				t.Fatal("prepare applied burn")
			}
			due := now + int64(arrow.ActionCastingTimeMs) + 1
			if mode == "cancelled" {
				effect := rt.effects.Snapshot(testDivision, c.Name)[0]
				rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: fire.ID, InstanceToken: effect.InstanceToken}.Encode())
			}
			if mode == "expired" {
				due = now + 5001
			}
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			rt.TickHook()(due)
			after, _ = rt.Monsters.Get(testDivision, target)
			if burnSlot(after).Active != (mode == "active") {
				t.Fatal("release ignored current imbue lifecycle", mode, burnSlot(after))
			}
			if c.MissionInventory[1].StackCount != 1 || *c.CurrentMP != 127 {
				t.Fatal("release cost duplicated or missing", *c.CurrentMP)
			}
		})
	}
}

/*
================
TestInstantImbuePreservesOpenAttackAndContinuation
================
*/
func TestInstantImbuePreservesOpenAttackAndContinuation(t *testing.T) {
	for _, mode := range []string{"accepted", "unlearned", "mp", "target"} {
		t.Run(mode, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			fire := installFireImbue(t, rt, c)
			smash := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
			rt.deps.SkillData().(staticSkillSource)[smash.ID] = smash
			c.Skills = append(c.Skills, smash.ID)
			open := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: smash.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
			open = assertAndSeparateActionSession(t, open)
			if len(open.Frames) != 1 || len(open.Frames[0].Payload) != 19 {
				t.Fatal("missing cast preparation")
			}
			attackToken := binary.LittleEndian.Uint32(open.Frames[0].Payload[10:])
			releasePreparedSkillForTest(t, rt, clock.NowMs()+int64(smash.ActionCastingTimeMs)+1)
			intent := rt.combatIntentSnapshot()[0]
			request := wire.SkillAction{ActionId: fire.ID}
			switch mode {
			case "unlearned":
				c.Skills = []uint32{2, smash.ID}
			case "mp":
				c.CurrentMP = testInt64(0)
			case "target":
				request.HasTarget, request.TargetGid = true, target.Gid
			}
			mp := *c.CurrentMP
			result := rt.HandleTargetInteract(testDivision, c, request.Encode())
			if !rt.combatIntentIsCurrent(intent) || !rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("instant request replaced the open attack or its continuation", mode)
			}
			if mode == "accepted" {
				assertOpcodes(t, result.Frames, wire.OpSkillCastResult, simulation.OpVitalsUpdate, wire.OpSkillEffectControl, wire.OpSkillEffectControl, wire.OpAttachedEffect)
				if *c.CurrentMP != mp-int64(fire.Consumption.MP) || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
					t.Fatal("concurrent imbue not committed exactly once")
				}
				for _, f := range result.Frames {
					if f.Opcode == wire.OpSkillEffectControl {
						offset := 1
						if f.Payload[0] == 2 {
							offset = 2
						}
						if binary.LittleEndian.Uint32(f.Payload[offset:]) == attackToken {
							t.Fatal("imbue retired the attack token")
						}
					}
				}
			} else if *c.CurrentMP != mp || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
				t.Fatal("refused instant request changed resources/effects")
			}
			life, _ := smash.ActionLifecycleMs()
			life++
			rt.TickHook()(clock.NowMs() + int64(life))
			before := rt.castTokenCounter
			rt.TickHook()(clock.NowMs() + int64(life) + 1)
			if rt.castTokenCounter != before+1 || len(rt.combatIntentSnapshot()) != 1 {
				t.Fatal("instant request lost native basic continuation", mode)
			}
		})
	}
}

/*
==================
TestColdImbueAddsDamageAndChills

SKILL_CH_COLD_GIGONGTA_A_01 is the Fire Force's shape with fz 30 5 and
fb 30 25 in place of bu: the imbued hit adds the imbue damage and rolls
the cold rider, never a burn.
==================
*/
func TestColdImbueAddsDamageAndChills(t *testing.T) {
	imbued := func(roll bool) (*Runtime, *enterworld.Character, monster.Instance) {
		rt, _, c, target := newCombatTestRuntime(t, 100000)
		row := shippedOffense(t, "SKILL_CH_COLD_GIGONGTA_A_01")
		if !row.Imbue.Pinned {
			t.Fatal("shipped Cold Force not admitted", row)
		}
		rt.deps.SkillData().(staticSkillSource)[row.ID] = row
		c.Skills = append(c.Skills, row.ID)
		c.CurrentMP = testInt64(1000)
		if r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode()); len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
			t.Fatalf("activation: %+v", r)
		}
		if roll {
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
		}
		return rt, c, target
	}

	plain, _, plainC, plainTarget := newCombatTestRuntime(t, 100000)
	hit := plain.HandleTargetInteract(testDivision, plainC, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: plainTarget.Gid}.Encode())
	_, baseDamage, _ := assertSkillDamageOpen(t, hit.Frames, 2, enterworld.ObjectIDForCharacter(plainC), plainTarget.Gid)
	rt, c, target := imbued(false)
	hit = rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	_, imbuedDamage, _ := assertSkillDamageOpen(t, hit.Frames, 2, enterworld.ObjectIDForCharacter(c), target.Gid)
	if imbuedDamage <= baseDamage {
		t.Fatalf("imbue damage missing: %d vs %d", imbuedDamage, baseDamage)
	}

	rt, c, target = imbued(true)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.Abnormal == nil || after.Abnormal.Mask&(abnormal.Freeze.Bit()|abnormal.Frostbite.Bit()) == 0 || burnSlot(after).Active {
		t.Fatalf("cold rider not rolled: %+v", after.Abnormal)
	}
}

/*
==================
TestLightningImbueChainsTheHit

SKILL_CH_LIGHTNING_GIGONGTA_A_01 carries efr 1 6 35 2 80 24. A basic
attack with no area of its own selects its victims with it (586E1B): the
primary keeps the whole hit, the chained victim (flag 1) loses the physical
lane and takes only the imbue damage at 20 % (80 % reduction), on every
impact. The es rider rolls on the victims.
==================
*/
func TestLightningImbueChainsTheHit(t *testing.T) {
	rt, targets := areaFixture(t, 100000)
	c := rt.findCharacter(testDivision, "asd2")
	table := rt.deps.SkillData().(staticSkillSource)
	row := shippedOffense(t, "SKILL_CH_LIGHTNING_GIGONGTA_A_01")
	if !row.Imbue.Pinned || row.Imbue.Area.Radius != 35 || row.Imbue.Area.Shape != 6 {
		t.Fatal("shipped Lightning Force not admitted", row.Imbue)
	}
	table[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(1000)
	if rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode()); len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("imbue not active")
	}
	basic := table[2]
	basic.ReplacementPinned, basic.Replacement.MatchesExecutionSelector = true, true // as SKILL_CH_SWORD_BASE_01 ships
	basic.Attack.ImpactCount = 2
	table[2] = basic
	roll := func() (uint32, error) { return 10, nil }
	rt.CombatRoll = roll

	hit := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	var p []byte
	for _, f := range hit.Frames {
		if f.Opcode == wire.OpSkillCastResult {
			p = f.Payload
		}
	}
	if len(p) < 21 || p[19] != 2 || p[20] != 2 || binary.LittleEndian.Uint32(p[21:]) != targets[0].Gid {
		t.Fatalf("chained result % X %+v", p, hit)
	}
	damage := func(victim, impact int) uint32 {
		return binary.LittleEndian.Uint32(p[21+victim*22+4+impact*9+1:]) >> 8
	}
	chainedGID := binary.LittleEndian.Uint32(p[21+22:])
	chained, _ := rt.Monsters.Get(testDivision, chainedGID)

	// The chained share, from the imbue's own lane alone.
	attacker, _, _ := rt.playerCombatStats(testDivision, c)
	defender, _ := combat.MonsterInstanceStats(chained)
	extra, err := combat.ResolveOutcome(attacker, defender, row.Imbue.Attack, roll, true, false)
	if err != nil {
		t.Fatal(err)
	}
	want := uint32(uint64(uint16(extra.Damage))*uint64(basic.Attack.Value5)/100) * 20 / 100
	for impact := range 2 {
		if damage(0, impact) <= damage(1, impact) || damage(1, impact) != want {
			t.Fatalf("impact %d: primary %d chained %d, want chained %d", impact, damage(0, impact), damage(1, impact), want)
		}
	}
	if chained.CurrentHP != 100000-2*want {
		t.Fatalf("chained HP %d, want %d", chained.CurrentHP, 100000-2*want)
	}
}
