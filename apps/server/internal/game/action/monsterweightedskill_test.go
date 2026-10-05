/*
===========================================================================

monsterweightedskill_test.go - the weighted default-skill choice (561B00)

CAISkill_Basic_SelectConditionalThenWeighted weighs each skill by its
authored AI weight plus half the reach it has to spare over the target's
distance, then draws rand() % (total+1) and takes the first skill whose
running total reaches the draw.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
weightedSample

The uniform sample whose CRT word is word.
================
*/
func weightedSample(word uint32) float64 {
	return (float64(word) + 0.5) / 32768
}

/*
================
TestMonsterWeightedSkillAddsSpareReachToAuthoredWeight

A short skill (weight 100, range 10) is out of reach at distance 40; a long
one (weight 20, range 50) has 5+50+10-40 = 25 spare and gains 12. Totals run
100 then 132, so draw 100 is the short skill and 101 the long one.
================
*/
func TestMonsterWeightedSkillAddsSpareReachToAuthoredWeight(t *testing.T) {
	actor := monster.Instance{Ref: monster.MonsterRef{BodyRadius: 10}}
	short := enterworld.SkillRow{ID: 1, AIWeight: 100, ActionRange: 10}
	long := enterworld.SkillRow{ID: 2, AIWeight: 20, ActionRange: 50}
	unweighted := enterworld.SkillRow{ID: 3, AIWeight: 0, ActionRange: 500}
	skills := []enterworld.SkillRow{unweighted, short, long}
	target := &simulation.AttackTarget{Distance: 40, BodyRadius: 5}
	for _, tc := range []struct {
		word uint32
		want uint32
	}{{0, 1}, {100, 1}, {101, 2}, {132, 2}, {133, 1}} {
		skill, ok := monsterWeightedSkill(actor, skills, simulation.AttackPick{Sample: weightedSample(tc.word), Target: target})
		if !ok || skill.ID != tc.want {
			t.Fatalf("word %d chose %d/%v, want %d", tc.word, skill.ID, ok, tc.want)
		}
	}
	// Without a target only the authored weights count: totals 100, 120.
	skill, ok := monsterWeightedSkill(actor, skills, simulation.AttackPick{Sample: weightedSample(121)})
	if !ok || skill.ID != 1 {
		t.Fatalf("untargeted word 121 chose %d/%v", skill.ID, ok)
	}
	if _, ok := monsterWeightedSkill(actor, []enterworld.SkillRow{unweighted}, simulation.AttackPick{Sample: .5}); ok {
		t.Fatal("a weightless skill list chose a skill")
	}
}
