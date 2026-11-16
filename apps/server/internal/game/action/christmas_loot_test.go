/*
===========================================================================

christmas_loot_test.go - the winter event's snow flakes: who drops them,
what So-ok gives for them, when they leave

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/clientdata"
)

// testSnowflakeRef is the fixture's snow flake reference.
const testSnowflakeRef = 28244

/*
================
christmasLootRuntime

The Halloween fixture's items plus the snow flake, the socks and two of
So-ok's gifts; the Christmas window is the Halloween tests' 1970 one.
================
*/
func christmasLootRuntime(t *testing.T, c *enterworld.Character, on bool) *Runtime {
	t.Helper()
	items := halloweenTestItems()
	etc := func(refObjID uint32, codename string, typeIDs [4]int64, maxStack float64) *enterworld.ItemRef {
		return &enterworld.ItemRef{
			RefObjID: refObjID, Codename: codename, TypeIDs: typeIDs,
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": maxStack}),
		}
	}
	items[progression.ChristmasSnowflakeCodename] = etc(testSnowflakeRef, progression.ChristmasSnowflakeCodename, [4]int64{3, 3, 9, 0}, 250)
	items[progression.ChristmasSockCodename] = etc(23463, progression.ChristmasSockCodename, [4]int64{3, 3, 9, 0}, 50)
	items["ITEM_MALL_DAMAGE_ABS_20P_SCROLL"] = etc(5001, "ITEM_MALL_DAMAGE_ABS_20P_SCROLL", [4]int64{3, 3, 13, 1}, 50)
	items["ITEM_COS_C_RUDOLPH"] = etc(5002, "ITEM_COS_C_RUDOLPH", [4]int64{3, 3, 1, 2}, 1)
	rt, _ := newTestRuntime(c, items)
	setting := "off"
	if on {
		setting = "on"
	}
	t.Setenv(progression.EnvChristmasEvent, setting)
	t.Setenv(progression.EnvChristmasEventWindow, halloweenTestWindow)
	rt.Christmas = progression.ChristmasEventFromEnv()
	return rt
}

/*
================
flakeRow
================
*/
func flakeRow(slot, count int64) enterworld.InventoryRow {
	return enterworld.InventoryRow{
		Slot: slot, RefObjID: testSnowflakeRef, Codename: progression.ChristmasSnowflakeCodename,
		TypeFlags: wire.PackTypeFlags(3, 3, 9, 0), StackCount: count,
	}
}

/*
================
TestSnowSlaveLeavesItsSnowflakeEveryTime

A Snow Slave's kill plan is its flake, owned by the killer, whatever the
draw (the owner's 100%), and nothing else; with the event off the plan is
the native one and holds no flake.
================
*/
func TestSnowSlaveLeavesItsSnowflakeEveryTime(t *testing.T) {
	c := testCharacter()
	level := int64(90)
	c.Level = &level
	slave := monster.Instance{Ref: monster.MonsterRef{Codename: progression.ChristmasSnowSlaveCodename, Level: 1, MaxHP: 5}}
	rt := christmasLootRuntime(t, c, true)
	for _, draw := range []uint32{0, 16384, 32767} {
		rt.DropRoll = func() (uint32, error) { return draw, nil }
		drops := rt.planMonsterKillLoot(c, slave, monster.Pose{}, rt.Now().UnixMilli())
		if len(drops) != 1 || drops[0].Codename != progression.ChristmasSnowflakeCodename || drops[0].OwnerJID != enterworld.ObjectIDForCharacter(c) {
			t.Fatalf("draw %d: Snow Slave left %+v, want its one flake", draw, drops)
		}
	}
	off := christmasLootRuntime(t, c, false)
	off.DropRoll = func() (uint32, error) { return 0, nil }
	for _, drop := range off.planMonsterKillLoot(c, slave, monster.Pose{}, off.Now().UnixMilli()) {
		if drop.Codename == progression.ChristmasSnowflakeCodename {
			t.Fatal("event off: a Snow Slave left a flake")
		}
	}
}

/*
================
TestChristmasExchangeAtSoOk

So-ok offers the winter row only while the event runs; fewer than ten
flakes answer QUEST_E; one click trades every ten flakes in the bag, a
gift each, with the talk line alone (no banner), and the odd flakes
stay.
================
*/
func TestChristmasExchangeAtSoOk(t *testing.T) {
	c := testCharacter()
	off := christmasLootRuntime(t, c, false)
	if options := off.ChristmasTalkOptions(halloweenSoOkCodename, c); len(options) != 0 {
		t.Fatal("options offered with the event off")
	}
	rt := christmasLootRuntime(t, c, true)
	options := rt.ChristmasTalkOptions(halloweenSoOkCodename, c)
	if len(options) != 1 || options[0].Codename != ChristmasExchangeService ||
		options[0].TitleSymbol != christmasTalkOption || options[0].PromptSymbol != christmasTalkPrompt {
		t.Fatal("christmas talk options", options)
	}
	if options := rt.ChristmasTalkOptions("NPC_CH_SMITH", c); len(options) != 0 {
		t.Fatal("options offered at another NPC")
	}

	c.MissionInventory = []enterworld.InventoryRow{flakeRow(20, 9)}
	frames, err := rt.ExchangeChristmasSnowflakes(c, halloweenSoOkCodename)
	if err != nil || len(frames) != 1 || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(christmasTalkNoFlakes)) {
		t.Fatal("too few flakes", frames, err)
	}

	c.MissionInventory = []enterworld.InventoryRow{flakeRow(20, 25)}
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	frames, err = rt.ExchangeChristmasSnowflakes(c, halloweenSoOkCodename)
	if err != nil || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(christmasTalkGranted)) {
		t.Fatal("granted exchange", frames, err)
	}
	for _, frame := range frames {
		if frame.Opcode == chat.OpChatBroadcast {
			t.Fatal("a banner went out; the talk line alone answers")
		}
	}
	if bagCount(c, progression.ChristmasSnowflakeCodename) != 5 || bagCount(c, "ITEM_MALL_DAMAGE_ABS_20P_SCROLL") != 2 {
		t.Fatal("one click, a gift per ten flakes", c.MissionInventory)
	}
}

/*
================
TestEveryChristmasGiftIsInShippedData

Every gift by codename: a typo would roll a gift the grant then refuses.
The flake itself is imported from the later client, so it is not here.
================
*/
func TestEveryChristmasGiftIsInShippedData(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(clientdata.RetailTextdataDir(t))
	for _, gift := range progression.ChristmasGifts() {
		if ref, ok := items.ItemRefByCodename(gift.Codename); !ok || ref == nil {
			t.Fatalf("%s is not in shipped data", gift.Codename)
		}
	}
}

/*
================
TestChristmasTokensLeaveAtEntry

A world entry outside the event takes every flake from the bag and from
a pet's bag; inside it the flakes stay. The Red Santa Socks leave at
every entry. Every other row stays.
================
*/
func TestChristmasTokensLeaveAtEntry(t *testing.T) {
	character := func() *enterworld.Character {
		c := testCharacter()
		c.MissionInventory = []enterworld.InventoryRow{
			flakeRow(20, 7),
			{Slot: 21, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 5},
			{Slot: 22, RefObjID: 23463, Codename: progression.ChristmasSockCodename, TypeFlags: wire.PackTypeFlags(3, 3, 9, 0), StackCount: 2},
		}
		c.ActiveCOS = &enterworld.CharacterCOS{Container: &domain.COSContainer{Rows: []enterworld.InventoryRow{flakeRow(0, 3)}}}
		return c
	}
	on := character()
	if !christmasLootRuntime(t, on, true).RemoveExpiredEventItems(on) || bagCount(on, progression.ChristmasSnowflakeCodename) != 7 ||
		bagCount(on, progression.ChristmasSockCodename) != 0 {
		t.Fatal("event on: flakes must stay, socks leave", on.MissionInventory)
	}
	off := character()
	if !christmasLootRuntime(t, off, false).RemoveExpiredEventItems(off) {
		t.Fatal("event off: no removal reported")
	}
	if bagCount(off, progression.ChristmasSnowflakeCodename) != 0 || len(off.ActiveCOS.Container.Rows) != 0 || bagCount(off, "ITEM_ETC_HP_POTION_01") != 5 {
		t.Fatal("event off: bag", off.MissionInventory, off.ActiveCOS.Container.Rows)
	}
	if christmasLootRuntime(t, off, false).RemoveExpiredEventItems(off) {
		t.Fatal("a second entry reported a removal")
	}
}
