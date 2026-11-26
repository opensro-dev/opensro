/*
===========================================================================

fortresshit_test.go - a fortress weapon's admitted hits

The hit-outcome routine's fortress group: an admitted fortress-weapon
swing deals 1 to an ordinary target and the attack-power draw to a
fortress structure, never the lane formula.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
armFortressHammer

Swaps the fixture character's sword for a fortress hammer (weapon kind
TID4 16) in the item source and in slot 6.
================
*/
func armFortressHammer(t *testing.T, rt *Runtime, c *enterworld.Character) {
	t.Helper()
	hammer := &enterworld.ItemRef{
		RefObjID: 19227, Codename: "ITEM_FORT_FORTRESS_HAMMER_06",
		TypeIDs: [4]int64{3, 1, 6, 16},
	}
	rt.deps.(*enterworld.Deps).Items.(staticItemSource)[hammer.Codename] = hammer
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == 6 {
			c.MissionInventory[i].RefObjID = hammer.RefObjID
			c.MissionInventory[i].Codename = hammer.Codename
			c.MissionInventory[i].TypeFlags = hammer.TypeFlags()
		}
	}
	if !rt.fortressArmed(testDivision, c.Name) {
		t.Fatal("fixture character is not fortress-armed")
	}
}

/*
================
fortressHitSkill

A fortress weapon's basic attack shape: the formula inputs are large,
so the lane formula would deal hundreds if it ever ran.
================
*/
func fortressHitSkill() enterworld.SkillRow {
	return enterworld.SkillRow{ID: 25040, Attack: enterworld.SkillAttack{
		Present: true, Flags: 5, Percent: 100, Min: 10, Max: 20, Value5: 100, ImpactCount: 1,
	}}
}

/*
================
TestFortressWeaponHitDealsOneToAnOrdinaryTarget
================
*/
func TestFortressWeaponHitDealsOneToAnOrdinaryTarget(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	armFortressHammer(t, rt, c)
	draws := 0
	rt.CombatRoll = func() (uint32, error) { draws++; return 137, nil }
	attacker := combat.Stats{Level: 1,
		PhysicalAttackMin: 100, PhysicalAttackMax: 200, MagicalAttackMin: 500, MagicalAttackMax: 600}
	out, err := rt.resolveCombatRequest(combatRequest{
		actor: criticalActor{division: testDivision, character: c.Name},
		skill: fortressHitSkill(), attacker: attacker, defender: combat.Stats{}, lanes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Defender.Damage != 1 || out.Defender.Blocked {
		t.Fatalf("fortress hit = %+v, want a plain 1", out.Defender)
	}
	if draws != 0 {
		t.Fatalf("fortress hit on an ordinary target consumed %d draws, want 0", draws)
	}
}

/*
================
TestFortressWeaponHitDrawsAgainstAStructure
================
*/
func TestFortressWeaponHitDrawsAgainstAStructure(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	armFortressHammer(t, rt, c)
	draws := 0
	rt.CombatRoll = func() (uint32, error) { draws++; return 137, nil }
	attacker := combat.Stats{Level: 1,
		PhysicalAttackMin: 100, PhysicalAttackMax: 200, MagicalAttackMin: 500, MagicalAttackMax: 600}
	out, err := rt.resolveCombatRequest(combatRequest{
		actor:    criticalActor{division: testDivision, character: c.Name},
		skill:    fortressHitSkill(), attacker: attacker,
		defender: combat.Stats{Structure: true}, lanes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 100 + 137 % (200 - 100 + 1) = 136.
	if out.Defender.Damage != 136 || out.Defender.Blocked {
		t.Fatalf("fortress hit on a structure = %+v, want the draw 136", out.Defender)
	}
	if draws != 1 {
		t.Fatalf("structure draw consumed %d draws, want 1", draws)
	}
}

/*
================
TestFortressWeaponBlockedHitTakesNoDamageDraw

A blocked fortress hit is a bare blocked record even against a
structure: the damage draw never happens.
================
*/
func TestFortressWeaponBlockedHitTakesNoDamageDraw(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	armFortressHammer(t, rt, c)
	draws := 0
	rt.CombatRoll = func() (uint32, error) { draws++; return 137, nil }
	attacker := combat.Stats{Level: 1,
		PhysicalAttackMin: 100, PhysicalAttackMax: 200, MagicalAttackMin: 500, MagicalAttackMax: 600}
	out, err := rt.resolveCombatRequest(combatRequest{
		actor:    criticalActor{division: testDivision, character: c.Name},
		skill:    fortressHitSkill(), attacker: attacker,
		defender: combat.Stats{Structure: true, BlockRate: 100}, lanes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Defender.Blocked || out.Defender.Damage != 0 {
		t.Fatalf("blocked fortress hit = %+v, want blocked with no damage", out.Defender)
	}
	if draws != 0 {
		t.Fatalf("blocked fortress hit consumed %d draws, want 0", draws)
	}
}

/*
================
TestFortressWeaponImpactAddsNoImbueShare

A fortress-armed attacker holding a live imbue (lit before the hammer)
still deals only the group's amount: no imbue share joins the hit, and
a standing wall absorbs nothing from a lane-less hit.
================
*/
func TestFortressWeaponImpactAddsNoImbueShare(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	if result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode()); result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	armFortressHammer(t, rt, c)
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, rt.Now().UnixMilli()); !imbue.Pinned {
		t.Fatal("imbue not live")
	}
	rt.CombatRoll = func() (uint32, error) { return 137, nil }
	attacker := combat.Stats{Level: 1,
		PhysicalAttackMin: 500, PhysicalAttackMax: 600, MagicalAttackMin: 500, MagicalAttackMax: 600}
	wall := &enterworld.SkillWall{Mask: 0xc, Pool: 1000}
	out, err := rt.resolvePlayerImpactBehindWall(testDivision, c.Name, fortressHitSkill(), attacker, combat.Stats{}, rt.Now().UnixMilli(), false, wall)
	if err != nil {
		t.Fatal(err)
	}
	if out.Defender.Damage != 1 {
		t.Fatalf("fortress hit behind a wall dealt %d, want the group's 1 alone", out.Defender.Damage)
	}
	if out.Absorbed != 0 {
		t.Fatalf("wall absorbed %d from a lane-less hit, want 0", out.Absorbed)
	}
}
