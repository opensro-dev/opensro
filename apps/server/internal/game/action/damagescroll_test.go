/*
===========================================================================

damagescroll_test.go - the damage scroll raises imbued hits

ITEM_MALL_DAMAGE_INC_20P_SCROLL casts SKILL_MALL_DAMAGE_INC_20P_SCROLL_01:
cbuf, dura 30 min, dru 20/20. dru adds word 0 to the basic damage rates
(0x80/0x81) and word 1 to the skill rates (0x82/0x83) (595A97). A fire
imbue's share is its own attack block with the basic bit set, so the scroll
must raise it as it raises the weapon's own hit.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

const (
	// damageScrollRoll is a fixed mid-range roll, so two hits compare exactly.
	damageScrollRoll = 16383
	// damageScrollToken is any cast token for the installed scroll effect.
	damageScrollToken = 77
)

/*
================
imbuedHit

One basic attack under a fire imbue, with or without the damage scroll,
the scroll used before or after the imbue.
================
*/
func imbuedHit(t *testing.T, imbue, scroll, scrollFirst bool) uint32 {
	t.Helper()
	rt, _, c, target := newCombatTestRuntime(t, 10_000_000)
	rt.CombatRoll = func() (uint32, error) { return damageScrollRoll, nil }
	castImbue := func() {
		row := installFireImbue(t, rt, c)
		rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	}
	if imbue && !scrollFirst {
		castImbue()
	}
	if scroll {
		buff := shippedOffense(t, "SKILL_MALL_DAMAGE_INC_20P_SCROLL_01")
		if !buff.TimedEffect.Pinned || !buff.TimedEffect.ItemProgram {
			t.Fatalf("shipped damage scroll skill not admitted: %+v", buff.TimedEffect)
		}
		rt.deps.SkillData().(staticSkillSource)[buff.ID] = buff
		if _, applied := rt.commitCharacterEffect(testDivision, c, buff, damageScrollToken,
			statuseffect.StateActive, false, EffectPresentation{Phase: 2}, rt.Now().UnixMilli()); !applied {
			t.Fatal("the damage scroll's effect did not install")
		}
	}
	if imbue && scrollFirst {
		castImbue()
	}
	hit := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
	_, damage, _ := assertSkillDamageOpen(t, hit.Frames, 2, enterworld.ObjectIDForCharacter(c), target.Gid)
	return damage
}

/*
================
TestDamageScrollRaisesImbuedHits
================
*/
func TestDamageScrollRaisesImbuedHits(t *testing.T) {
	plain, scrolled := imbuedHit(t, false, false, false), imbuedHit(t, false, true, false)
	imbued, both := imbuedHit(t, true, false, false), imbuedHit(t, true, true, false)
	scrollFirst := imbuedHit(t, true, true, true)
	t.Logf("plain %d, scroll %d, imbue %d, imbue then scroll %d, scroll then imbue %d", plain, scrolled, imbued, both, scrollFirst)
	if scrolled <= plain {
		t.Fatalf("the scroll did not raise a plain hit: %d -> %d", plain, scrolled)
	}
	if imbued <= plain {
		t.Fatalf("the imbue did not raise a plain hit: %d -> %d", plain, imbued)
	}
	if both <= imbued || both <= scrolled {
		t.Fatalf("scroll and imbue do not stack: imbue %d, scroll %d, both %d", imbued, scrolled, both)
	}
	if scrollFirst != both {
		t.Fatalf("the order of use changes the stack: imbue then scroll %d, scroll then imbue %d", both, scrollFirst)
	}
}
