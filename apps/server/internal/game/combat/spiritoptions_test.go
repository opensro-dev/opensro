/*
===========================================================================

spiritoptions_test.go - the worn Devil's Spirit's stat share and reqi gate

Port-only, behind SRO_NASRUN (#3456). The action owner sets
AvatarInventory.Spirit while a Spirit is worn and awake; combat reads it.

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
spiritFixture

A level-10 character wearing nothing but a Spirit whose effect is the
given one (nil: dormant or off).
================
*/
func spiritFixture(effect *domain.SpiritEffect) (*domain.Character, Catalogs) {
	c := &domain.Character{ID: 4040, Level: pointer(10), Strength: pointer(40), Intellect: pointer(30),
		AvatarInventory: &domain.AvatarInventory{Capacity: 5, Spirit: effect}}
	return c, Catalogs{Items: itemRefs{}, MagicOptions: optionRows()}
}

/*
================
TestSpiritRaisesTheGaugesByPercentAndTheBlockFlat

A +3 Spirit's 18 % lands on the STR and INT gauge nodes (0x34, 0x35) in
percent-sum mode, and its block 1 on the block rate; without it, none.
================
*/
func TestSpiritRaisesTheGaugesByPercentAndTheBlockFlat(t *testing.T) {
	c, catalogs := spiritFixture(nil)
	bare, _, err := PlayerStats(c, catalogs)
	if err != nil {
		t.Fatal(err)
	}
	c, catalogs = spiritFixture(&domain.SpiritEffect{Slot: 4, Plus: 3, HPPercent: 18, MPPercent: 18, Block: 1})
	worn, _, err := PlayerStats(c, catalogs)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []uint16{0x34, 0x35} {
		b, _ := bare.Param(p)
		w, _ := worn.Param(p)
		if want := b * 1.18; w < want-0.01 || w > want+0.01 {
			t.Fatalf("param %#x worn %v, want bare %v * 1.18", p, w, b)
		}
	}
	bareHP, _ := bare.Param(3)
	wornHP, _ := worn.Param(3)
	if wornHP <= bareHP {
		t.Fatalf("max HP worn %v, bare %v: the share never reached the gauge", wornHP, bareHP)
	}
	if worn.BlockRate != bare.BlockRate+1 {
		t.Fatalf("block rate worn %v, want bare %v + 1", worn.BlockRate, bare.BlockRate)
	}
}

/*
================
TestReqiAvatarSocketNeedsTheAttachedSpirit

reqi 14 1 (avatar socket 4) admits only while the Spirit is attached;
another value never does.
================
*/
func TestReqiAvatarSocketNeedsTheAttachedSpirit(t *testing.T) {
	req := enterworld.SkillReqi{Present: true, Count: 1, Pairs: [5]enterworld.SkillReqiPair{{Kind: 14, Value: 1}}}
	c, catalogs := spiritFixture(nil)
	if ReqiRefusal(c, catalogs.Items, req) == 0 {
		t.Fatal("no Spirit: reqi 14 1 admitted")
	}
	c.AvatarInventory.Spirit = &domain.SpiritEffect{Slot: 4}
	if code := ReqiRefusal(c, catalogs.Items, req); code != 0 {
		t.Fatalf("attached Spirit: refused %#x", code)
	}
	req.Pairs[0].Value = 2
	if ReqiRefusal(c, catalogs.Items, req) == 0 {
		t.Fatal("reqi 14 2 admitted")
	}
}
