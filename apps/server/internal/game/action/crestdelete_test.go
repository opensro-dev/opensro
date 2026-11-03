/*
===========================================================================

crestdelete_test.go - a crest delete use clears the stored crest

#2282 (delete half): ITEM_ETC_GUILD_CREST_DELETE clears the guild's
CrestParam and ITEM_ETC_UNION_CREST_DELETE the union's Crest, each
behind the ordinary item use (0x75BD -> 0xB5BD) with a 0x34F3 crest
update behind the answer. With no crest standing the use is refused
and the item is not spent. These tests drive the real guild store
door and the real union authority.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/union"
)

/*
================
crestDeleteFixture

The guild-manager fixture's leader, holding one crest delete item of
the named kind in bag slot 21, with refs swapped for a mutable source.
Returns the runtime, the character and its guild's id.
================
*/
func crestDeleteFixture(t *testing.T, codename string, param1 float64) (*doorRuntime, *enterworld.Character, int64) {
	t.Helper()
	d := guildManagerFixture(t)
	c := d.character
	if c.GuildID == nil {
		t.Fatal("fixture: leader has no guild")
	}
	refs := testItems()
	refs[codename] = &enterworld.ItemRef{Codename: codename, RefObjID: 9269, Country: 3,
		TypeIDs:      [4]int64{3, 3, 12, 2},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": param1, "itemParam2_2a0": 2})}
	d.rt.deps.(*enterworld.Deps).Items = refs
	d.authority.UpdateCharacter(c, "crest-delete-fixture", func() bool {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21,
			RefObjID: 9269, Codename: codename, TypeFlags: refs[codename].TypeFlags(), StackCount: 1})
		return true
	})
	return d, c, *c.GuildID
}

/*
================
setGuildCrest

Writes the guild's stored crest through its own door.
================
*/
func setGuildCrest(t *testing.T, d *doorRuntime, c *enterworld.Character, crest uint32) {
	t.Helper()
	_, refusal := d.authority.Guilds().UpdateGuildAs(testDivision, c.ID, "crest-delete-fixture",
		domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.CrestParam = crest
			return g, m, true
		})
	if refusal.Refused() {
		t.Fatalf("seed crest: %v", refusal)
	}
}

/*
================
useCrestDelete

The ordinary 0x75BD use of the bag-slot-21 delete item.
================
*/
func useCrestDelete(t *testing.T, d *doorRuntime, c *enterworld.Character, codename string) OpResult {
	t.Helper()
	ref := d.rt.deps.ItemReferences().(staticItemSource)[codename]
	return d.rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload())
}

/*
================
crestUpdateFrame

The result's 0x34F3 frame payload, or nil.
================
*/
func crestUpdateFrame(result OpResult) []byte {
	for _, frame := range result.Frames {
		if frame.Opcode == guild.OpCrestUpdate {
			return frame.Payload
		}
	}
	return nil
}

/*
================
TestGuildCrestDeleteClearsTheStoredCrest
================
*/
func TestGuildCrestDeleteClearsTheStoredCrest(t *testing.T) {
	d, c, guildID := crestDeleteFixture(t, "ITEM_ETC_GUILD_CREST_DELETE", 1)
	setGuildCrest(t, d, c, 4242)
	result := useCrestDelete(t, d, c, "ITEM_ETC_GUILD_CREST_DELETE")
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("guild crest delete refused: %+v", result)
	}
	if _, ok := inventoryRowAt(c, 21); ok {
		t.Fatal("delete item not spent")
	}
	record, _, ok := d.authority.Guilds().Guild(testDivision, guildID)
	if !ok || record.CrestParam != 0 {
		t.Fatalf("stored crest = %d (ok %t), want cleared", record.CrestParam, ok)
	}
	payload := crestUpdateFrame(result)
	if len(payload) < 7 || payload[0] != 1 || binary.LittleEndian.Uint32(payload[1:]) != uint32(guildID) {
		t.Fatalf("crest update = % X, want a guild row for %d", payload, guildID)
	}
	if got := binary.LittleEndian.Uint32(payload[len(payload)-4:]); got != 0 {
		t.Fatalf("crest update carries crest %d, want 0", got)
	}
}

/*
================
TestGuildCrestDeleteWithoutACrestSpendsNothing
================
*/
func TestGuildCrestDeleteWithoutACrestSpendsNothing(t *testing.T) {
	d, c, guildID := crestDeleteFixture(t, "ITEM_ETC_GUILD_CREST_DELETE", 1)
	result := useCrestDelete(t, d, c, "ITEM_ETC_GUILD_CREST_DELETE")
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("crestless delete answered: %+v", result)
	}
	if row, ok := inventoryRowAt(c, 21); !ok || row.StackCount != 1 {
		t.Fatal("crestless delete spent the item")
	}
	if _, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok {
		t.Fatal("guild lost")
	}
	if payload := crestUpdateFrame(result); payload != nil {
		t.Fatalf("crestless delete published % X", payload)
	}
}

/*
================
TestUnionCrestDeleteClearsTheStoredCrest
================
*/
func TestUnionCrestDeleteClearsTheStoredCrest(t *testing.T) {
	d, c, guildID := crestDeleteFixture(t, "ITEM_ETC_UNION_CREST_DELETE", 2)
	store := d.authority.Alliances()
	record := domain.AllianceRecord{AllianceID: guildID, Crest: 777}
	record.Guilds[0] = guildID
	record.Guilds[1] = guildID + 500
	if err := store.SaveAlliance(testDivision, record, true); err != nil {
		t.Fatal(err)
	}
	d.rt.Unions = union.New()
	if err := d.rt.Unions.Restore(testDivision, store); err != nil {
		t.Fatal(err)
	}
	result := useCrestDelete(t, d, c, "ITEM_ETC_UNION_CREST_DELETE")
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("union crest delete refused: %+v", result)
	}
	stored, err := store.Alliances(testDivision)
	if err != nil || len(stored) != 1 || stored[0].Crest != 0 {
		t.Fatalf("stored union = %+v (%v), want crest cleared", stored, err)
	}
	payload := crestUpdateFrame(result)
	if len(payload) < 6 || payload[0] != 2 || binary.LittleEndian.Uint32(payload[1:]) != uint32(guildID) {
		t.Fatalf("crest update = % X, want a union row for %d", payload, guildID)
	}
	if got := binary.LittleEndian.Uint32(payload[len(payload)-4:]); got != 0 {
		t.Fatalf("crest update carries crest %d, want 0", got)
	}
}

/*
================
TestCrestRegisterItemStaysUnusable

The register twins (param2 1) keep the unsupported answer: their flow
is the owner-blocked half of #2282 (#2272), not this slice.
================
*/
func TestCrestRegisterItemStaysUnusable(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	refs := testItems()
	refs["ITEM_ETC_GUILD_CREST_USER"] = &enterworld.ItemRef{Codename: "ITEM_ETC_GUILD_CREST_USER", RefObjID: 9268, Country: 3,
		TypeIDs:      [4]int64{3, 3, 12, 2},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 1, "itemParam2_2a0": 1})}
	d.rt.deps.(*enterworld.Deps).Items = refs
	d.authority.UpdateCharacter(c, "crest-delete-fixture", func() bool {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21,
			RefObjID: 9268, Codename: "ITEM_ETC_GUILD_CREST_USER", TypeFlags: refs["ITEM_ETC_GUILD_CREST_USER"].TypeFlags(), StackCount: 1})
		return true
	})
	ref := refs["ITEM_ETC_GUILD_CREST_USER"]
	result := d.rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("register use answered: %+v", result)
	}
	if _, ok := inventoryRowAt(c, 21); !ok {
		t.Fatal("register use spent the item")
	}
}
