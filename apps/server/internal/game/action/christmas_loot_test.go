/*
===========================================================================

christmas_loot_test.go - Red Santa Socks: who drops them, what So-ok gives

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
christmasLootRuntime

The Halloween fixture's items plus the socks, the Santa Hats and the
Santa Wings; the Christmas window is the Halloween tests' 1970 one.
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
	items[progression.ChristmasSockCodename] = etc(23463, progression.ChristmasSockCodename, [4]int64{3, 3, 9, 0}, 50)
	items["ITEM_ETC_E041225_SANTA_HAT_M"] = etc(3654, "ITEM_ETC_E041225_SANTA_HAT_M", [4]int64{3, 1, 13, 0}, 1)
	items["ITEM_ETC_E041225_SANTA_HAT_F"] = etc(3655, "ITEM_ETC_E041225_SANTA_HAT_F", [4]int64{3, 1, 13, 0}, 1)
	items["ITEM_ETC_E041225_SANTA_WINGS"] = etc(3653, "ITEM_ETC_E041225_SANTA_WINGS", [4]int64{3, 3, 3, 0}, 50)
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
sockRow
================
*/
func sockRow(slot, count int64) enterworld.InventoryRow {
	return enterworld.InventoryRow{
		Slot: slot, RefObjID: 23463, Codename: progression.ChristmasSockCodename,
		TypeFlags: wire.PackTypeFlags(3, 3, 9, 0), StackCount: count,
	}
}

/*
================
TestChristmasSockDrops

Each winter monster leaves its count of socks; any other monster and
the event off leave none.
================
*/
func TestChristmasSockDrops(t *testing.T) {
	rt := christmasLootRuntime(t, testCharacter(), true)
	now := time.UnixMilli(1_000_000)
	for codename, want := range map[string]int{
		progression.ChristmasSnowSlaveCodename:    progression.ChristmasSnowSlaveSocks,
		progression.ChristmasKnightCodename(90):   progression.ChristmasKnightSocks,
		progression.ChristmasPrincessCodename(90): progression.ChristmasPrincessSocks,
		"MOB_CH_MANGNYANG":                        0,
	} {
		socks := rt.christmasSockDrops(monster.Instance{Ref: monster.MonsterRef{Codename: codename}}, simulation.Spawn{}, "Hunter", now)
		if len(socks) != want {
			t.Fatalf("%s left %d socks, want %d", codename, len(socks), want)
		}
		for _, sock := range socks {
			if sock.Codename != progression.ChristmasSockCodename {
				t.Fatalf("%s left %s", codename, sock.Codename)
			}
		}
	}
	off := christmasLootRuntime(t, testCharacter(), false)
	if socks := off.christmasSockDrops(monster.Instance{Ref: monster.MonsterRef{Codename: progression.ChristmasSnowSlaveCodename}}, simulation.Spawn{}, "Hunter", now); len(socks) != 0 {
		t.Fatalf("event off: %d socks", len(socks))
	}
}

/*
================
TestSnowSlaveLeavesOnlyItsSocks

A Snow Slave's kill plan is its socks, owned by the killer, whatever the
level gap; with the event off the plan is the native one.
================
*/
func TestSnowSlaveLeavesOnlyItsSocks(t *testing.T) {
	c := testCharacter()
	level := int64(90)
	c.Level = &level
	slave := monster.Instance{Ref: monster.MonsterRef{Codename: progression.ChristmasSnowSlaveCodename, Level: 1, MaxHP: 5}}
	rt := christmasLootRuntime(t, c, true)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	drops := rt.planMonsterKillLoot(c, slave, monster.Pose{}, rt.Now().UnixMilli())
	if len(drops) != progression.ChristmasSnowSlaveSocks {
		t.Fatalf("Snow Slave left %d items, want only its %d socks", len(drops), progression.ChristmasSnowSlaveSocks)
	}
	for _, drop := range drops {
		if drop.Codename != progression.ChristmasSockCodename || drop.OwnerJID != enterworld.ObjectIDForCharacter(c) {
			t.Fatalf("Snow Slave left %s owned by %d", drop.Codename, drop.OwnerJID)
		}
	}
	off := christmasLootRuntime(t, c, false)
	off.DropRoll = func() (uint32, error) { return 0, nil }
	for _, drop := range off.planMonsterKillLoot(c, slave, monster.Pose{}, off.Now().UnixMilli()) {
		if drop.Codename == progression.ChristmasSockCodename {
			t.Fatal("event off: a Snow Slave left socks")
		}
	}
}

/*
================
TestChristmasWinterMonsterKeepsTheLevelGap

A level-90 hunter's roll on a level-1 winter unique: its ordinary loot
meets the native threshold, which admits nothing, with the event on as
off. Its socks come from outside admission (TestSnowSlaveLeavesOnlyItsSocks).
================
*/
func TestChristmasWinterMonsterKeepsTheLevelGap(t *testing.T) {
	knight := monster.Instance{Ref: monster.MonsterRef{Codename: progression.ChristmasKnightCodename(20), Level: 1}}
	for _, enabled := range []bool{true, false} {
		rt := christmasLootRuntime(t, testCharacter(), enabled)
		rt.DropRoll = dropRollSequence(0)
		if rt.admitMonsterDrop(90, knight) {
			t.Fatalf("event %v: the winter unique's loot skipped the native gap", enabled)
		}
	}
}

/*
================
TestChristmasExchangeAtSoOk

So-ok offers the one shipped row only while the event runs; no pair
answers the no-socks line; one click trades every pair in the bag, one
present each, with the talk line alone (no banner); a female
character's costume piece is the female one.
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

	c.MissionInventory = nil
	frames, err := rt.ExchangeChristmasSocks(c, halloweenSoOkCodename)
	if err != nil || len(frames) != 1 || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(christmasTalkNoSocks)) {
		t.Fatal("no-socks exchange", frames, err)
	}

	c.MissionInventory = []enterworld.InventoryRow{sockRow(20, 4)}
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	frames, err = rt.ExchangeChristmasSocks(c, halloweenSoOkCodename)
	if err != nil || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(christmasTalkGranted)) {
		t.Fatal("granted exchange", frames, err)
	}
	for _, frame := range frames {
		if frame.Opcode == chat.OpChatBroadcast {
			t.Fatal("a banner went out; the talk line alone answers")
		}
	}
	if bagCount(c, progression.ChristmasSockCodename) != 0 || bagCount(c, "ITEM_ETC_E051123_EVATION_SCROLL") != 4*3 {
		t.Fatal("one click, a present per pair", c.MissionInventory)
	}

	female := testCharacter()
	gender := int64(enterworld.GenderFemale)
	female.Gender = &gender
	female.ModelCodename = "CHAR_CH_WOMAN_ADVENTURER"
	female.MissionInventory = []enterworld.InventoryRow{sockRow(20, progression.ChristmasSocksPerGift)}
	rt = christmasLootRuntime(t, female, true)
	rt.DropRoll = func() (uint32, error) { return 28016, nil } // roll 85.5 of 100: the red hat band (84, 87]
	if _, err := rt.ExchangeChristmasSocks(female, halloweenSoOkCodename); err != nil {
		t.Fatal(err)
	}
	if bagCount(female, "ITEM_ETC_E041225_SANTA_HAT_F") != 1 || bagCount(female, progression.ChristmasSockCodename) != 0 {
		t.Fatal("female hat", female.MissionInventory)
	}
}

/*
================
TestEveryChristmasItemIsInShippedData

The socks and every present by codename, both sexes of a costume piece:
a typo would roll a present the grant then refuses.
================
*/
func TestEveryChristmasItemIsInShippedData(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(clientdata.RetailTextdataDir(t))
	codenames := []string{progression.ChristmasSockCodename}
	for _, gift := range progression.ChristmasGifts() {
		codenames = append(codenames, gift.Codename)
		if gift.FemaleCodename != "" {
			codenames = append(codenames, gift.FemaleCodename)
		}
	}
	for _, codename := range codenames {
		if ref, ok := items.ItemRefByCodename(codename); !ok || ref == nil {
			t.Fatalf("%s is not in shipped data", codename)
		}
	}
}

/*
================
TestChristmasSocksLeaveWhenTheEventIsOff

With the event off, a world entry takes every pair from the bag and
from a pet's bag, and leaves every other row; with it on, nothing moves.
================
*/
func TestChristmasSocksLeaveWhenTheEventIsOff(t *testing.T) {
	character := func() *enterworld.Character {
		c := testCharacter()
		c.MissionInventory = []enterworld.InventoryRow{sockRow(20, 7), {Slot: 21, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 5}}
		c.ActiveCOS = &enterworld.CharacterCOS{Container: &domain.COSContainer{Rows: []enterworld.InventoryRow{sockRow(0, 3)}}}
		return c
	}
	on := character()
	if christmasLootRuntime(t, on, true).RemoveExpiredEventItems(on) || bagCount(on, progression.ChristmasSockCodename) != 7 {
		t.Fatal("event on: socks removed")
	}
	off := character()
	if !christmasLootRuntime(t, off, false).RemoveExpiredEventItems(off) {
		t.Fatal("event off: no removal reported")
	}
	if bagCount(off, progression.ChristmasSockCodename) != 0 || len(off.ActiveCOS.Container.Rows) != 0 || bagCount(off, "ITEM_ETC_HP_POTION_01") != 5 {
		t.Fatal("event off: bag", off.MissionInventory, off.ActiveCOS.Container.Rows)
	}
	if christmasLootRuntime(t, off, false).RemoveExpiredEventItems(off) {
		t.Fatal("a second entry reported a removal")
	}
}
