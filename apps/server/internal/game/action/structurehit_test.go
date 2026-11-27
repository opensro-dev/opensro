/*
===========================================================================

structurehit_test.go - hits on fortress structures

The hit-outcome routine's target-side group: a hit on a fortress
structure is the attacker's attack-power draw scaled by the worn
weapon's kind factor, never the lane formula and never the fortress
weapon group's flat 1.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// structureHitAttacker spans 199 on both attack pairs with a critical
// rate that would consume a roll if the group ever let one through,
// so a draw count of 1 pins the group's place ahead of the rolls.
func structureHitAttacker() combat.Stats {
	return combat.Stats{Level: 1, CriticalRate: 50,
		PhysicalAttackMin: 9900, PhysicalAttackMax: 10099,
		MagicalAttackMin: 20900, MagicalAttackMax: 21099}
}

/*
================
TestStructureHitByOrdinaryWeaponDealsFactoredDraw

The fixture sword (kind 2) hits a structure for the physical draw
times its 0.30 factor: draw 100 over a 199 span lands on 10000, so
the hit is 3000 with exactly one draw consumed.
================
*/
func TestStructureHitByOrdinaryWeaponDealsFactoredDraw(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	draws := 0
	rt.CombatRoll = func() (uint32, error) { draws++; return 100, nil }
	out, err := rt.resolveCombatRequest(combatRequest{
		actor: criticalActor{division: testDivision, character: c.Name},
		skill: fortressHitSkill(), attacker: structureHitAttacker(),
		defender: combat.Stats{Structure: true}, lanes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Defender.Damage != 3000 || out.Defender.Blocked {
		t.Fatalf("structure hit = %+v, want a plain 3000", out.Defender)
	}
	if draws != 1 {
		t.Fatalf("structure hit consumed %d draws, want 1", draws)
	}
}

/*
================
TestStructureHitByFortressWeaponDealsTheBareDraw

A fortress weapon (kind 16) against a structure is in the target-side
group, not its own: the bare physical draw, not the flat 1 it deals
every other target.
================
*/
func TestStructureHitByFortressWeaponDealsTheBareDraw(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	armFortressHammer(t, rt, c)
	draws := 0
	rt.CombatRoll = func() (uint32, error) { draws++; return 100, nil }
	out, err := rt.resolveCombatRequest(combatRequest{
		actor: criticalActor{division: testDivision, character: c.Name},
		skill: fortressHitSkill(), attacker: structureHitAttacker(),
		defender: combat.Stats{Structure: true}, lanes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Defender.Damage != 10000 || out.Defender.Blocked {
		t.Fatalf("fortress hit on a structure = %+v, want the bare draw 10000", out.Defender)
	}
	if draws != 1 {
		t.Fatalf("fortress hit on a structure consumed %d draws, want 1", draws)
	}
}

/*
================
TestStructureHitBlockedDealsNothing

A blocked structure hit is the bare type-2 record: no damage, and
neither the block shortcut nor the group consumes a draw.
================
*/
func TestStructureHitBlockedDealsNothing(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	draws := 0
	rt.CombatRoll = func() (uint32, error) { draws++; return 100, nil }
	out, err := rt.resolveCombatRequest(combatRequest{
		actor: criticalActor{division: testDivision, character: c.Name},
		skill: fortressHitSkill(), attacker: structureHitAttacker(),
		defender: combat.Stats{Structure: true, BlockRate: 100}, lanes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Defender.Blocked || out.Defender.Damage != 0 {
		t.Fatalf("blocked structure hit = %+v, want blocked with no damage", out.Defender)
	}
	if draws != 0 {
		t.Fatalf("blocked structure hit consumed %d draws, want 0", draws)
	}
}

/*
================
TestStructureHitAddsNoImbueShare

A structure hit with a live imbue lit deals only the group's amount:
no imbue share joins it, a standing wall absorbs nothing from a
lane-less hit, and no extra draw is consumed.
================
*/
func TestStructureHitAddsNoImbueShare(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	if result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode()); result.DiagnosticRefusal != "" {
		t.Fatal(result.DiagnosticRefusal)
	}
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, rt.Now().UnixMilli()); !imbue.Pinned {
		t.Fatal("imbue not live")
	}
	draws := 0
	rt.CombatRoll = func() (uint32, error) { draws++; return 100, nil }
	wall := &enterworld.SkillWall{Mask: 0xc, Pool: 1000}
	out, err := rt.resolvePlayerImpactBehindWall(testDivision, c.Name, fortressHitSkill(), structureHitAttacker(), combat.Stats{Structure: true}, rt.Now().UnixMilli(), false, wall)
	if err != nil {
		t.Fatal(err)
	}
	if out.Defender.Damage != 3000 {
		t.Fatalf("structure hit behind a wall dealt %d, want the group's 3000 alone", out.Defender.Damage)
	}
	if out.Absorbed != 0 {
		t.Fatalf("wall absorbed %d from a lane-less hit, want 0", out.Absorbed)
	}
	if draws != 1 {
		t.Fatalf("structure hit behind a wall consumed %d draws, want 1", draws)
	}
}
