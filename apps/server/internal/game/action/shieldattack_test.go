/*
===========================================================================

shieldattack_test.go - Flying Heaven Art through the real cast and keeper

The shipped SKILL_CH_SWORD_SHIELDPD_A_01 row (spda 17 27, reqi shield) cast
with a shipped shield equipped: physical defense falls and physical attack
rises by the authored percents of the shield's own defense.

===========================================================================
*/

package action

import (
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	flyingHeavenA1 = "SKILL_CH_SWORD_SHIELDPD_A_01"
	highShield     = "ITEM_CH_SHIELD_09_C"
)

/*
================
TestFlyingHeavenArtTradesShieldDefense
================
*/
func TestFlyingHeavenArtTradesShieldDefense(t *testing.T) {
	rt, clock, c, _, _ := shieldFixture(t)
	// A tier 9 shield: the starter shield's defense is too small for
	// either percent to reach one point.
	shield, found := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t)).ItemRefByCodename(highShield)
	if !found {
		t.Fatalf("missing %s", highShield)
	}
	rt.deps.ItemReferences().(staticItemSource)[shield.Codename] = shield
	art := shippedOffense(t, flyingHeavenA1)
	spda := art.TimedEffect.ShieldAttack
	if !art.TimedEffect.Pinned || !spda.Present {
		t.Fatalf("Flying Heaven Art not admitted: %+v", art.TimedEffect)
	}
	rt.deps.SkillData().(staticSkillSource)[art.ID] = art
	c.Skills = append(c.Skills, art.ID)
	c.CurrentMP = testInt64(10000)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 7, RefObjID: shield.RefObjID, Codename: shield.Codename, TypeFlags: shield.TypeFlags(),
		VarianceBits: "0", Plus: 5, Durability: 1, StackCount: 1,
	})
	pd, ok := combat.ShieldPhysicalDefense(c, rt.statCatalogs().Items)
	if !ok || pd <= 0 {
		t.Fatalf("shield defense %v %v", pd, ok)
	}
	lowered := math.Trunc(float64(spda.DefensePercent) * pd / 100)
	raised := math.Trunc(float64(spda.AttackPercent) * pd / 100)
	if lowered <= 0 || raised <= 0 {
		t.Fatalf("fixture shield too weak to test: pd %v", pd)
	}
	before, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}

	if out := castSelf(rt, c, art.ID); out.DiagnosticRefusal != "" {
		t.Fatalf("Flying Heaven Art refused: %+v", out)
	}
	if art.ActionCastingTimeMs > 0 {
		clock.Advance(time.Duration(art.ActionCastingTimeMs+1) * time.Millisecond)
		rt.advanceProjectileCasts(clock.NowMs())
	}
	if !hasSkillEffect(rt, c.Name, art.ID) {
		t.Fatal("no instance installed")
	}
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if after.PhysicalDefense != before.PhysicalDefense-lowered ||
		after.PhysicalAttackMin != before.PhysicalAttackMin+raised || after.PhysicalAttackMax != before.PhysicalAttackMax+raised {
		t.Fatalf("pd %v: defense %v -> %v (want -%v), attack %v-%v -> %v-%v (want +%v)", pd,
			before.PhysicalDefense, after.PhysicalDefense, lowered,
			before.PhysicalAttackMin, before.PhysicalAttackMax, after.PhysicalAttackMin, after.PhysicalAttackMax, raised)
	}
}
