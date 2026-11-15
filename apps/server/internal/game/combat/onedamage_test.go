/*
===========================================================================

onedamage_test.go - tests for OneDamageCap (the Christmas Snow Slave)

Port-only, not native: a monster marked OneDamageHits takes exactly 1
from any hit that deals damage, whatever the attacker's power.

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestOneDamageHitsLandForOne

The same 150-damage hits of TestResolvePhysicalAndMagicalNormalLanes land
for 1 on a OneDamageHits monster, on either lane and on both at once; the
lane split still sums to the hit.
================
*/
func TestOneDamageHitsLandForOne(t *testing.T) {
	attacker := Stats{
		Level: 1, MaxLevel: 1,
		Strength: 32, Intellect: 32,
		PhysicalAttackMin: 100, PhysicalAttackMax: 200,
		MagicalAttackMin: 100, MagicalAttackMax: 200,
		HitRate: 50,
	}
	defender, err := MonsterStats(monster.MonsterRef{Level: 1, CombatPinned: true, OneDamageHits: true})
	if err != nil {
		t.Fatal(err)
	}
	if !defender.OneDamageHits {
		t.Fatal("MonsterStats dropped the ref's OneDamageHits")
	}
	for _, flags := range []uint32{physicalAttackFlag, magicalAttackFlag, physicalAttackFlag | magicalAttackFlag} {
		rolls := &sequenceRoll{values: []uint32{0, 0, 0, 0, 0, 0, 0, 0}}
		result, err := Resolve(attacker, defender, enterworld.SkillAttack{Present: true, Flags: flags, Percent: 100}, rolls.next)
		if err != nil {
			t.Fatalf("flags %#x: %v", flags, err)
		}
		if result.Damage != 1 || result.PhysicalDamage+result.MagicalDamage != 1 {
			t.Fatalf("flags %#x: result %+v, want one point", flags, result)
		}
	}

	// An ordinary monster still takes the full hit.
	ordinary, err := MonsterStats(monster.MonsterRef{Level: 1, CombatPinned: true})
	if err != nil {
		t.Fatal(err)
	}
	rolls := &sequenceRoll{values: []uint32{0, 0, 0, 0}}
	result, err := Resolve(attacker, ordinary, enterworld.SkillAttack{Present: true, Flags: physicalAttackFlag, Percent: 100}, rolls.next)
	if err != nil {
		t.Fatal(err)
	}
	if result.Damage <= 1 {
		t.Fatalf("ordinary monster took %d", result.Damage)
	}
}

/*
================
TestOneDamageCap

The cap the fixed-damage and life-steal hits go through: any damage
becomes 1 on a OneDamageHits defender, a zero stays zero, and any other
defender keeps the damage.
================
*/
func TestOneDamageCap(t *testing.T) {
	marked := Stats{OneDamageHits: true}
	for _, c := range []struct {
		defender      Stats
		damage, wantD uint32
	}{{marked, 5000, 1}, {marked, 1, 1}, {marked, 0, 0}, {Stats{}, 5000, 5000}} {
		if got := OneDamageCap(c.defender, c.damage); got != c.wantD {
			t.Fatalf("OneDamageCap(%v, %d) = %d, want %d", c.defender.OneDamageHits, c.damage, got, c.wantD)
		}
	}
}
