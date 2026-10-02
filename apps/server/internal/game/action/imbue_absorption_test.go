/*
===========================================================================

imbue_absorption_test.go - original attack kind through imbue resolution

58F312 passes the original skill to 40E830; 410BB0 reads its kind bits.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

/*
================
TestImbueUsesOriginalAttackKind
================
*/
func TestImbueUsesOriginalAttackKind(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, clock.NowMs()); !imbue.Pinned {
		t.Fatal("fixture imbue did not activate")
	}
	a := combat.Stats{Level: 1, MaxLevel: 1, Strength: 32, Intellect: 32,
		PhysicalAttackMin: 100, PhysicalAttackMax: 100, MagicalAttackMin: 100, MagicalAttackMax: 100}
	d := combat.Stats{Level: 1, MaxLevel: 1, MagicalBasicTaken: 0.5, MagicalSkillTaken: 0.25}
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true, Flags: 5, Percent: 100, Value5: 100}}
	base, err := rt.resolvePlayerImpact(testDivision, c.Name, skill, a, d, clock.NowMs(), false)
	if err != nil {
		t.Fatal(err)
	}
	skill.Attack.Flags = 6
	trained, err := rt.resolvePlayerImpact(testDivision, c.Name, skill, a, d, clock.NowMs(), false)
	if err != nil {
		t.Fatal(err)
	}
	if trained.MagicalDamage >= base.MagicalDamage {
		t.Fatalf("imbue magical damage: basic=%d skill=%d; original skill must select the stronger reduction", base.MagicalDamage, trained.MagicalDamage)
	}
}
