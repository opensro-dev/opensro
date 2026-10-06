/*
===========================================================================

mercenary_attribute_test.go - atomic attribute purchases and native vitals

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestMercenaryAttributePurchaseResetAndRefusal
================
*/
func TestMercenaryAttributePurchaseResetAndRefusal(t *testing.T) {
	d, item := mercenaryFixture(t)
	c := d.character
	gold := int64(2000000)
	d.authority.UpdateCharacter(c, "attribute-fixture", func() bool { c.Gold = &gold; return true })
	_, refusal := d.authority.Guilds().UpdateGuildAs(testDivision, c.ID, "attribute-fixture", domain.GuildAuthorization{},
		func(g domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.GP = 200000
			return g, members, true
		})
	if refusal.Refused() {
		t.Fatal(refusal)
	}
	useSummonerFixture(t, d.rt, c, 23, item)
	for _, step := range []struct {
		attribute, code, flags uint8
		gold                   int64
	}{
		{8, 0, 8, 1500000}, {8, 2, 8, 1500000}, {2, 0, 10, 1000000},
		{4, 2, 10, 1000000}, {0, 0, 0, 500000}, {0, 2, 0, 500000},
	} {
		out := d.rt.HandleMercenaryAttribute(testDivision, c, wire.NewWriter(5).U32(guildManagerGid).U8(step.attribute).Payload())
		want := []byte{1, step.attribute}
		if step.code != 0 {
			want = []byte{2, step.code}
		}
		if len(out.Frames) == 0 || out.Frames[0].Opcode != opMercenaryAttributeReply || !bytes.Equal(out.Frames[0].Payload, want) {
			t.Fatalf("attribute %d reply %+v", step.attribute, out)
		}
		if goldOf(c) != uint64(step.gold) {
			t.Fatalf("attribute %d gold %d", step.attribute, goldOf(c))
		}
		for _, pet := range c.Mercenaries {
			ref, _ := d.rt.cosReference(pet)
			maxHP := float32(100)
			if step.flags&8 != 0 {
				maxHP = 135
			}
			if pet.MercenaryAttributes != step.flags || pet.CurrentHP != 100 || cosParameter(ref, pet, nil, abnormalMaxHPParam) != maxHP {
				t.Fatalf("attribute %d soldier %+v", step.attribute, pet)
			}
		}
	}
}

/*
================
TestMercenaryAttributeSummonInitializesVitalsBeforeModifiers
================
*/
func TestMercenaryAttributeSummonInitializesVitalsBeforeModifiers(t *testing.T) {
	d, item := mercenaryFixture(t)
	_, refusal := d.authority.Guilds().UpdateGuildAs(testDivision, d.character.ID, "attribute-fixture", domain.GuildAuthorization{},
		func(g domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.Byte10 = 8
			return g, members, true
		})
	if refusal.Refused() {
		t.Fatal(refusal)
	}
	useSummonerFixture(t, d.rt, d.character, 23, item)
	for _, pet := range d.character.Mercenaries {
		ref, _ := d.rt.cosReference(pet)
		if pet.CurrentHP != 100 || cosParameter(ref, pet, nil, abnormalMaxHPParam) != 135 {
			t.Fatalf("spawn order %+v", pet)
		}
	}
}
