/*
===========================================================================

overkill_test.go - preserve full hit feedback while the HP owner clamps debit

BUG-041 reported 55 damage against a 55-HP monster. Exercise both directions
through their committed result producers so a fatal hit cannot become an HP
measurement on the wire.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCommittedMonsterHitPreservesOverkill

The shared projection serves immediate, area, multi-impact and persistent hits.
================
*/
func TestCommittedMonsterHitPreservesOverkill(t *testing.T) {
	const hp = uint32(55)
	const damage = uint32(1000)
	rt, _, _, target := newCombatTestRuntime(t, hp)
	results := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, hp,
		[]simulation.MonsterDamagePlan{{GID: target.Gid, Damage: damage}, {GID: target.Gid, Damage: damage}})
	if len(results) != 1 {
		t.Fatalf("fatal sequence committed %d hits, want one", len(results))
	}
	result := results[0]
	impact := committedSkillImpact(combat.Result{Damage: damage, ResultFlags: 1}, result)
	if result.Applied != hp || result.CurrentHP != 0 || !result.Fatal || impact.Damage != damage {
		t.Fatalf("committed HP debit=%d HP=%d fatal=%v, wire damage=%d; want 55, 0, true, 1000", result.Applied, result.CurrentHP, result.Fatal, impact.Damage)
	}
}

/*
================
TestMonsterAttackPreservesOverkillFeedback

Compare identical attacks against a healthy and nearly dead character. The
victim's remaining HP changes the fatal flag and debit, never the rolled hit.
================
*/
func TestMonsterAttackPreservesOverkillFeedback(t *testing.T) {
	var expected uint32
	for _, hp := range []int64{100, 1} {
		rt, clock, character, attacker := newCombatTestRuntime(t, 100)
		character.CurrentHP = testInt64(hp)
		skills := rt.deps.SkillData().(staticSkillSource)
		skill := skills[2]
		skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 20, 20, 100
		skills[2] = skill
		attacker.Ref.DefaultSkillIDs[0] = 2
		result := rt.MonsterBasicAttack(testDivision, attacker, enterworld.ObjectIDForCharacter(character), 2, clock.NowMs())
		if !result.Accepted || len(result.Frames) == 0 {
			t.Fatalf("attack refused: %+v", result)
		}
		payload := result.Frames[0].Payload
		if len(payload) < 34 {
			t.Fatalf("missing hit record: % X", payload)
		}
		damage := binary.LittleEndian.Uint32(payload[26:]) >> 8
		if hp == 100 {
			expected = damage
			if expected <= 1 {
				t.Fatalf("fixture damage=%d cannot exercise overkill", expected)
			}
			continue
		}
		if damage != expected || enterworld.CurrentHP(character) != 0 || payload[25]&0x80 == 0 {
			t.Fatalf("fatal hit damage=%d HP=%d tag=%#x; want damage=%d HP=0 fatal", damage, enterworld.CurrentHP(character), payload[25], expected)
		}
	}
}
