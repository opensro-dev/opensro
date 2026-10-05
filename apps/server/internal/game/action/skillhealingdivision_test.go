/*
===========================================================================

skillhealingdivision_test.go - Healing Division heals the lowest HP ratio

Healing Division (efr(1,6,300,2,50,5) eshp heal mwhh): the party member
around the caster with the lowest HP ratio takes the whole heal, and the
member nearest it half of it.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// healingDivisionA1 is SKILL_EU_CLERIC_HEALA_DIVIDE_A_01.
const healingDivisionA1 = 10156

/*
================
TestHealingDivisionHealsTheLowestAndHalfTheNearest

Three members at 10, 50 and 60 % HP: the 10 % one takes the whole heal,
the 50 % one beside it half, and the 60 % one farther away nothing; the
full caster is not healed.
================
*/
func TestHealingDivisionHealsTheLowestAndHalfTheNearest(t *testing.T) {
	rt, _, c := concealmentFixture(t, healingDivisionA1)
	row := rt.deps.SkillData().(staticSkillSource)[healingDivisionA1]
	if !row.Recovery.LowestHealPinned || row.Abnormal.EffectArea.Shape != 6 || row.Abnormal.EffectArea.Reduction != 50 {
		t.Fatalf("Healing Division not admitted: %+v %+v", row.Recovery, row.Abnormal.EffectArea)
	}
	// 140 MP is beyond a level-1 caster; the cost is not under test.
	row.Consumption.MP = 10
	rt.deps.SkillData().(staticSkillSource)[healingDivisionA1] = row
	low := nearbyCharacter(rt, c, 12, "low", 20)
	near := nearbyCharacter(rt, c, 13, "near", 30)
	far := nearbyCharacter(rt, c, 14, "far", 200)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(low),
			enterworld.ObjectIDForCharacter(near), enterworld.ObjectIDForCharacter(far)}}}
	}
	type member struct {
		c           *enterworld.Character
		percent     int64
		max, before int64
	}
	members := []*member{{c: low, percent: 10}, {c: near, percent: 50}, {c: far, percent: 60}}
	for _, m := range members {
		m.max, _, _, _ = rt.playerKeeperVitals(testDivision, m.c)
		m.c.CurrentHP = testInt64(m.max * m.percent / 100)
		m.before = m.max * m.percent / 100
	}
	casterHP := enterworld.CurrentHP(c)
	whole, _, ok := rt.skillHealAmounts(testDivision, low, c, row, healCast)
	if !ok || whole == 0 {
		t.Fatal("no heal amount")
	}
	half, _, _ := rt.skillHealAmounts(testDivision, near, c, row, healCast)
	half = half * 50 / 100

	if r := castSelf(rt, c, healingDivisionA1); len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("Healing Division cast: %+v", r)
	}
	gain := func(m *member) int64 { return enterworld.CurrentHP(m.c) - m.before }
	if got, want := gain(members[0]), min(whole, members[0].max-members[0].before); got != want {
		t.Fatalf("lowest member healed %d, want %d", got, want)
	}
	if got, want := gain(members[1]), min(half, members[1].max-members[1].before); got != want || got == 0 {
		t.Fatalf("nearest member healed %d, want %d", got, want)
	}
	if got := gain(members[2]); got != 0 {
		t.Fatalf("far member healed %d", got)
	}
	if enterworld.CurrentHP(c) != casterHP {
		t.Fatalf("caster HP %d -> %d", casterHP, enterworld.CurrentHP(c))
	}
}
