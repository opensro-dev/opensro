/*
===========================================================================

critical_modifier_test.go - critical modifiers on direct, area and monster skills

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestCriticalModifierDirectMultiImpactWireAndHP

Synthetic modifier placement isolates each production route. The monster
case below also uses an unchanged authored cr skill. These are in-memory
integration tests with real serialization, not authenticated live captures.
==================
*/
func TestCriticalModifierDirectMultiImpactWireAndHP(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 10000)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.CriticalModifier = enterworld.SkillCriticalModifier{Present: true, Flat: 20}
	skills[2] = skill
	rt.CombatRoll = func() (uint32, error) { return 10, nil } // above base 3; below modified 23
	r := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult || len(r.Frames[0].Payload) != 43 {
		t.Fatalf("refused: %+v", r)
	}
	p := r.Frames[0].Payload
	a, b := binary.LittleEndian.Uint32(p[26:]), binary.LittleEndian.Uint32(p[35:])
	if uint8(a) != 2 || uint8(b) != 1 {
		t.Fatalf("modifier/multi-impact history missing: %x", p)
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if target.CurrentHP-after.CurrentHP != (a>>8)+(b>>8) {
		t.Fatal("wire damage differs from HP")
	}
	if got := rt.criticals.actors[criticalActor{division: testDivision, character: c.Name}][2].Threshold; got != -31 {
		t.Fatalf("per-impact accumulator = %d", got)
	}
}

/*
================
TestCriticalModifierAreaUsesOneActorSkillHistoryAcrossVictims
================
*/
func TestCriticalModifierAreaUsesOneActorSkillHistoryAcrossVictims(t *testing.T) {
	for _, physical := range []bool{true, false} {
		rt, targets := areaFixture(t, 100000)
		c := rt.findCharacter(testDivision, "asd2")
		skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
		skill.CriticalModifier = enterworld.SkillCriticalModifier{Present: true, Flat: 20}
		if physical {
			skill.Attack.Flags = 4
		}
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		c.CurrentMP = testInt64(1000)
		rt.CombatRoll = func() (uint32, error) { return 10, nil }
		r := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
		if len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult || len(r.Frames[0].Payload) < 60 {
			t.Fatalf("area refused %+v", r)
		}
		p := r.Frames[0].Payload
		for i := 0; i < 3; i++ {
			want := byte(1)
			if physical && i == 0 {
				want = 2
			}
			if p[26+13*i] != want {
				t.Fatalf("physical=%v victim %d flags=%d want=%d", physical, i, p[26+13*i], want)
			}
		}
		if !physical && len(rt.criticals.actors) != 0 {
			t.Fatal("magical lane consumed critical history")
		}
	}
}

/*
================
TestCriticalModifierAuthoredMonsterSkillReachesWire
================
*/
func TestCriticalModifierAuthoredMonsterSkillReachesWire(t *testing.T) {
	rt, clock, c, instance := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "MSKILL_QT_01_HUNARCHER_CLON_ATTACK01")
	if skill.CriticalModifier != (enterworld.SkillCriticalModifier{Present: true, Flat: 10}) {
		t.Fatal("lost authored cr pair")
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	instance.Ref.DefaultSkillIDs[0] = skill.ID
	c.CurrentHP = testInt64(100000)
	// The living gauge is keeper param 3, so a stored value above that
	// maximum is not the number a hit debits.
	_, _, beforeHP, _ := rt.playerKeeperVitals(testDivision, c)
	rt.CombatRoll = func() (uint32, error) { return 10, nil } // base monster rate=2, modified=12
	r := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(c), skill.ID, clock.NowMs())
	if !r.Accepted || len(r.Frames) == 0 || r.Frames[0].Opcode != wire.OpSkillCastResult {
		t.Fatalf("authored monster attack refused: %+v", r)
	}
	p := r.Frames[0].Payload
	offset := 26
	if len(p) == 19 {
		if _, _, hp, _ := rt.playerKeeperVitals(testDivision, c); hp != beforeHP {
			t.Fatal("preparation consumed HP")
		}
		released := rt.advanceMonsterCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
		if len(released) == 0 || released[0].Frames[0].Opcode != 0xb505 {
			t.Fatal("missing ranged release")
		}
		p = released[0].Frames[0].Payload
		offset = 17
	}
	if p[offset] != 2 {
		t.Fatalf("authored modifier did not affect actual result: %x", p)
	}
	var damage int64
	for i := 0; i < int(skill.Attack.ImpactCount); i++ {
		damage += int64(binary.LittleEndian.Uint32(p[offset+i*9:]) >> 8)
	}
	if beforeHP-*c.CurrentHP != min(beforeHP, damage) {
		t.Fatal("full monster hit does not account for the clamped HP debit")
	}
}

/*
================
TestCriticalModifierComboReadsEachExecutingStage
================
*/
func TestCriticalModifierComboReadsEachExecutingStage(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	root := installSwordCombo(t, rt, c)
	skills := rt.deps.SkillData().(staticSkillSource)
	tail := skills[root.ChainNext]
	tail.CriticalModifier = enterworld.SkillCriticalModifier{Present: true, Flat: 97} // guaranteed only on this stage
	skills[tail.ID] = tail
	rt.CombatRoll = func() (uint32, error) { return 10, nil }
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpSkillCastResult || result.Frames[0].Payload[26] != 1 {
		t.Fatal("root unexpectedly critical/refused")
	}
	seen := false
	for tick := 1; tick <= 30; tick++ {
		for _, route := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
			for _, frame := range route.Frames {
				if frame.Opcode != wire.OpSkillCastResult || len(frame.Payload) < 30 || frame.Payload[0] != 1 {
					continue
				}
				id := binary.LittleEndian.Uint32(frame.Payload[2:])
				want := byte(1)
				if id == tail.ID {
					want = 2
					seen = true
				}
				if frame.Payload[26] != want {
					t.Fatalf("stage %d flags %d want %d", id, frame.Payload[26], want)
				}
			}
		}
	}
	if !seen {
		t.Fatal("modified continuation never executed")
	}
}
