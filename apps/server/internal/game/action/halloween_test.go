/*
===========================================================================

halloween_test.go - the Halloween exchange, drop and cleanup behaviour

Every refusal is checked against an untouched bag, and the same cases
run with the event off, where the whole event must be invisible.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
halloweenTestItems

The retail refs the exchange and drop resolve: the box (3862), one
scroll, one grain potion and one avatar piece.
================
*/
func halloweenTestItems() staticItemSource {
	items := testItems()
	etc := func(refObjID uint32, codename string, maxStack float64) *enterworld.ItemRef {
		return &enterworld.ItemRef{
			RefObjID: refObjID, Codename: codename, TypeIDs: [4]int64{3, 3, 9, 0},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": maxStack}),
		}
	}
	items[progression.HalloweenBoxCodename] = etc(3862, progression.HalloweenBoxCodename, 50)
	items["ITEM_ETC_E051123_EVATION_SCROLL"] = etc(6098, "ITEM_ETC_E051123_EVATION_SCROLL", 50)
	items["ITEM_ETC_HP_SPOTION_01"] = etc(9, "ITEM_ETC_HP_SPOTION_01", 50)
	items["ITEM_MALL_AVATAR_M_HALLOWEEN"] = &enterworld.ItemRef{
		RefObjID: 23438, Codename: "ITEM_MALL_AVATAR_M_HALLOWEEN", TypeIDs: [4]int64{3, 1, 13, 0},
	}
	items["ITEM_ETC_E071030_M_EGHEAD"] = &enterworld.ItemRef{
		RefObjID: 23455, Codename: "ITEM_ETC_E071030_M_EGHEAD", TypeIDs: [4]int64{3, 1, 13, 0},
		NameStrID:    "SN_ITEM_ETC_E050526_M_EGHEAD",
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1, "canTrade": 1, "canSell": 1, "canBorrow": 255}),
	}
	items["ITEM_ETC_E071030_W_EGHEAD"] = &enterworld.ItemRef{
		RefObjID: 23459, Codename: "ITEM_ETC_E071030_W_EGHEAD", TypeIDs: [4]int64{3, 1, 13, 0},
		NameStrID:    "SN_ITEM_ETC_E050526_W_EGHEAD",
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1, "canTrade": 1, "canSell": 1, "canBorrow": 255}),
	}
	return items
}

/*
================
halloweenRuntime

A runtime whose event window covers the fixture clock (1970-01-01), so
each test moves the clock or the window, never both.
================
*/
func halloweenRuntime(t *testing.T, c *enterworld.Character, window string) (*Runtime, *fakeClock) {
	t.Helper()
	rt, clock := newTestRuntime(c, halloweenTestItems())
	t.Setenv(progression.EnvHalloweenEvent, "on")
	t.Setenv(progression.EnvHalloweenEventWindow, window)
	rt.Halloween = progression.HalloweenEventFromEnv()
	return rt, clock
}

const halloweenTestWindow = "1970-01-01T00:00:00Z/1970-01-03T00:00:00Z"

func halloweenBoxRow(slot int64, count int64) enterworld.InventoryRow {
	return enterworld.InventoryRow{
		Slot: slot, RefObjID: 3862, Codename: progression.HalloweenBoxCodename,
		TypeFlags: wire.PackTypeFlags(3, 3, 9, 0), StackCount: count,
	}
}

func bagCount(c *enterworld.Character, codename string) int {
	total := 0
	for _, row := range c.MissionInventory {
		if row.Codename == codename {
			total += int(row.StackCount)
		}
	}
	return total
}

func lastDialogue(t *testing.T, frames []wire.Frame) []byte {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	last := frames[len(frames)-1]
	if last.Opcode != wire.OpNpcDialog {
		t.Fatalf("last frame opcode %#x, want the talk answer", last.Opcode)
	}
	return last.Payload
}

/*
================
TestHalloweenExchangeRefusesAndGrantsByTheTalkLines
================
*/
func TestHalloweenExchangeRefusesAndGrantsByTheTalkLines(t *testing.T) {
	// No box: the no-box line, nothing else.
	c := testCharacter()
	c.MissionInventory = nil
	rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
	frames, err := rt.ExchangeHalloweenBox(c, halloweenSoOkCodename)
	if err != nil || len(frames) != 1 ||
		!bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogSymbol(halloweenTalkNoBox)) {
		t.Fatal("no-box exchange", frames, err)
	}

	// One box and a roll at the top of the range's first row: the
	// evasion scroll lands, the box leaves, the granted line closes.
	c = testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 1)}
	rt, _ = halloweenRuntime(t, c, halloweenTestWindow)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	frames, err = rt.ExchangeHalloweenBox(c, halloweenSoOkCodename)
	if err != nil || len(frames) < 2 ||
		!bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogSymbol(halloweenTalkGranted)) {
		t.Fatal("granted exchange", frames, err)
	}
	if bagCount(c, progression.HalloweenBoxCodename) != 0 || bagCount(c, "ITEM_ETC_E051123_EVATION_SCROLL") != 1 {
		t.Fatal("granted exchange bag", c.MissionInventory)
	}

	// A roll into the grain band pays ten potions, not one.
	c = testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 1)}
	rt, _ = halloweenRuntime(t, c, halloweenTestWindow)
	rt.DropRoll = func() (uint32, error) { return 22527, nil } // roll 55 of 80: the HP potion band (48, 61]
	frames, err = rt.ExchangeHalloweenBox(c, halloweenSoOkCodename)
	if err != nil || bagCount(c, "ITEM_ETC_HP_SPOTION_01") != 10 ||
		!bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogSymbol(halloweenTalkGranted)) {
		t.Fatal("grain exchange", frames, err, c.MissionInventory)
	}
}

/*
================
TestHalloweenExchangeFullBagRefusesBeforeTouchingTheBox
================
*/
func TestHalloweenExchangeFullBagRefusesBeforeTouchingTheBox(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = nil
	potion := halloweenTestItems()["ITEM_ETC_HP_SPOTION_01"]
	end := inventory.BagEnd(c)
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(end); slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: slot, RefObjID: potion.RefObjID, Codename: potion.Codename,
			TypeFlags: potion.TypeFlags(), StackCount: 50,
		})
	}
	// The box rides in the equipment band's shadow: move one filler row
	// out and put the box in its place so the bag is exactly full.
	c.MissionInventory[0] = halloweenBoxRow(c.MissionInventory[0].Slot, 1)
	rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	frames, err := rt.ExchangeHalloweenBox(c, halloweenSoOkCodename)
	if err != nil || len(frames) != 1 ||
		!bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogSymbol(halloweenTalkBagFull)) {
		t.Fatal("full-bag exchange", frames, err)
	}
	if bagCount(c, progression.HalloweenBoxCodename) != 1 {
		t.Fatal("full-bag refusal touched the box", c.MissionInventory)
	}
}

/*
================
TestHalloweenEventIsInvisibleWhileOffOrAway
================
*/
func TestHalloweenEventIsInvisibleWhileOffOrAway(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 2)}

	// The zero value (no env read at all): no option, an erroring
	// exchange, no cleanup, and the bag is untouched.
	rt, _ := newTestRuntime(c, halloweenTestItems())
	if options := rt.HalloweenTalkOptions(halloweenSoOkCodename, c); len(options) != 0 {
		t.Fatal("options offered with the event off")
	}
	if _, err := rt.ExchangeHalloweenBox(c, halloweenSoOkCodename); err == nil {
		t.Fatal("exchange ran with the event off")
	}
	if rt.RemoveExpiredHalloweenBoxes(c) || bagCount(c, progression.HalloweenBoxCodename) != 2 {
		t.Fatal("cleanup ran with no window configured")
	}

	// On, inside the window: So-ok offers the exchange row and the four
	// helmets of the (default male) fixture's sex; others do not.
	rt, _ = halloweenRuntime(t, c, halloweenTestWindow)
	options := rt.HalloweenTalkOptions(halloweenSoOkCodename, c)
	if len(options) != 5 {
		t.Fatal("halloween talk options", options)
	}
	option := options[0]
	if !option.Immediate || option.TitleSymbol != halloweenTalkOption || option.PromptSymbol != halloweenTalkPrompt {
		t.Fatal("halloween exchange row", option)
	}
	if options[1].Codename != HalloweenHelmetServicePrefix+"ITEM_ETC_E071030_M_EGHEAD" ||
		options[1].TitleSymbol != "SN_ITEM_ETC_E050526_M_EGHEAD" {
		t.Fatal("halloween helmet row", options[1])
	}
	if options := rt.HalloweenTalkOptions("NPC_CH_SMITH", c); len(options) != 0 {
		t.Fatal("options offered at another NPC")
	}
	if _, err := rt.ExchangeHalloweenBox(c, "NPC_CH_SMITH"); err == nil {
		t.Fatal("exchange ran at another NPC")
	}
	// Inside the window nothing is cleaned up.
	if rt.RemoveExpiredHalloweenBoxes(c) || bagCount(c, progression.HalloweenBoxCodename) != 2 {
		t.Fatal("cleanup ran inside the window")
	}

	// After the window the option closes and the boxes leave at entry;
	// every other row stays.
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 3,
	})
	rt, clock := halloweenRuntime(t, c, halloweenTestWindow)
	clock.now = time.Date(1970, 1, 4, 0, 0, 0, 0, time.UTC)
	if options := rt.HalloweenTalkOptions(halloweenSoOkCodename, c); len(options) != 0 {
		t.Fatal("options offered after the window")
	}
	if !rt.RemoveExpiredHalloweenBoxes(c) {
		t.Fatal("cleanup did not run after the window")
	}
	if bagCount(c, progression.HalloweenBoxCodename) != 0 || bagCount(c, "ITEM_ETC_HP_POTION_01") != 3 {
		t.Fatal("cleanup removed the wrong rows", c.MissionInventory)
	}
	if rt.RemoveExpiredHalloweenBoxes(c) {
		t.Fatal("cleanup reported a second removal")
	}
}

/*
================
TestHalloweenCleanupSweepsEveryContainer

A box parked in a companion's bag is a remaining box too: the active
companion, a mercenary and the bags riding inside bag and avatar summon
rows all give theirs up, and every other row stays.
================
*/
func TestHalloweenCleanupSweepsEveryContainer(t *testing.T) {
	bag := func() *domain.COSContainer {
		return &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{
			{Slot: 0, RefObjID: 3862, Codename: progression.HalloweenBoxCodename, StackCount: 3},
			{Slot: 1, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 2},
		}}
	}
	sites := []struct {
		name  string
		place func(c *enterworld.Character, box *domain.COSContainer)
	}{
		{"active companion", func(c *enterworld.Character, box *domain.COSContainer) {
			c.ActiveCOS = &enterworld.CharacterCOS{Container: box}
		}},
		{"mercenary", func(c *enterworld.Character, box *domain.COSContainer) {
			c.Mercenaries = []*enterworld.CharacterCOS{nil, {}, {Container: box}}
		}},
		{"bag summon row", func(c *enterworld.Character, box *domain.COSContainer) {
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
				Slot: 22, Summon: &enterworld.CharacterCOS{Container: box},
			})
		}},
		{"avatar summon row", func(c *enterworld.Character, box *domain.COSContainer) {
			c.AvatarInventory = &domain.AvatarInventory{Rows: []enterworld.InventoryRow{
				{Slot: 0, Summon: &enterworld.CharacterCOS{Container: box}},
			}}
		}},
	}
	for _, site := range sites {
		t.Run(site.name, func(t *testing.T) {
			c := &enterworld.Character{Name: "Hunter"}
			box := bag()
			site.place(c, box)
			rt, clock := halloweenRuntime(t, c, halloweenTestWindow)
			clock.now = time.Date(1970, 1, 4, 0, 0, 0, 0, time.UTC)
			if !rt.RemoveExpiredHalloweenBoxes(c) {
				t.Fatal("no removal reported for a parked box")
			}
			if len(box.Rows) != 1 || box.Rows[0].Codename != "ITEM_ETC_HP_POTION_01" {
				t.Fatal("container after cleanup", box.Rows)
			}
			if rt.RemoveExpiredHalloweenBoxes(c) {
				t.Fatal("cleanup reported a second removal")
			}
		})
	}
}

/*
================
TestHalloweenBoxDropGates
================
*/
func TestHalloweenBoxDropGates(t *testing.T) {
	c := testCharacter()
	rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
	ordinary := monster.Instance{Ref: monster.MonsterRef{MonsterType: 0}}
	unique := monster.Instance{Ref: monster.MonsterRef{MonsterType: 3}}
	at := simulation.Spawn{}

	rt.DropRoll = func() (uint32, error) { return 0, nil }
	if _, ok := rt.halloweenBoxDrop(c, ordinary, at, c.Name, time.UnixMilli(1_000_000)); !ok {
		t.Fatal("a winning draw dropped no box")
	}
	if _, ok := rt.halloweenBoxDrop(c, unique, at, c.Name, time.UnixMilli(1_000_000)); ok {
		t.Fatal("a unique dropped a box")
	}
	rt.DropRoll = func() (uint32, error) { return 32767, nil }
	if _, ok := rt.halloweenBoxDrop(c, ordinary, at, c.Name, time.UnixMilli(1_000_000)); ok {
		t.Fatal("a losing draw dropped a box")
	}
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	if _, ok := rt.halloweenBoxDrop(c, ordinary, at, c.Name, time.Date(1970, 1, 4, 0, 0, 0, 0, time.UTC)); ok {
		t.Fatal("a box dropped after the window")
	}
}

/*
================
TestHalloweenBoxDropCapAndGourd

The daily cap gates the rolled drop and the Gourd alike; a Gourd kill
pays on a losing draw, and a day that has rolled over at 04:00 UTC
opens the cap again. The count lands on the winner per committed box
and restarts when the settlement crosses the reset.
================
*/
func TestHalloweenBoxDropCapAndGourd(t *testing.T) {
	c := testCharacter()
	rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
	ordinary := monster.Instance{Ref: monster.MonsterRef{MonsterType: 0}}
	gourd := monster.Instance{Ref: monster.MonsterRef{Codename: halloweenGourdCodename, MonsterType: 0}}
	at := simulation.Spawn{}
	inside := time.Date(1970, 1, 1, 12, 0, 0, 0, time.UTC)

	// At the cap for the current day nothing mints, gourd included.
	c.HalloweenBoxDay = progression.HalloweenDayKey(inside)
	c.HalloweenBoxCount = progression.HalloweenBoxDailyCap
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	if _, ok := rt.halloweenBoxDrop(c, ordinary, at, c.Name, inside); ok {
		t.Fatal("a capped winner minted a box")
	}
	if _, ok := rt.halloweenBoxDrop(c, gourd, at, c.Name, inside); ok {
		t.Fatal("a capped winner's gourd paid a box")
	}

	// One under the cap: the gourd pays even on a losing draw.
	c.HalloweenBoxCount = progression.HalloweenBoxDailyCap - 1
	rt.DropRoll = func() (uint32, error) { return 32767, nil }
	box, ok := rt.halloweenBoxDrop(c, gourd, at, c.Name, inside)
	if !ok || box.Codename != progression.HalloweenBoxCodename {
		t.Fatal("the gourd paid no box on a losing draw", box, ok)
	}

	// The next day (after the 04:00 reset) the cap opens again.
	nextDay := time.Date(1970, 1, 2, 4, 0, 0, 0, time.UTC)
	c.HalloweenBoxDay = progression.HalloweenDayKey(inside)
	c.HalloweenBoxCount = progression.HalloweenBoxDailyCap
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	if _, ok := rt.halloweenBoxDrop(c, ordinary, at, c.Name, nextDay); !ok {
		t.Fatal("the cap stayed shut after the reset")
	}

	// Counting: two boxes land, then a settlement across 04:00 UTC
	// restarts the day before its box lands (reset crossed mid-wave).
	winner := testCharacter()
	rt.countHalloweenBoxes(winner, []grounditem.Item{
		{Codename: progression.HalloweenBoxCodename},
		{Codename: progression.HalloweenBoxCodename},
		{Codename: "ITEM_ETC_HP_POTION_01"},
	}, inside.UnixMilli())
	if winner.HalloweenBoxCount != 2 || winner.HalloweenBoxDay != progression.HalloweenDayKey(inside) {
		t.Fatal("count after two boxes", winner.HalloweenBoxDay, winner.HalloweenBoxCount)
	}
	rt.countHalloweenBoxes(winner, []grounditem.Item{{Codename: progression.HalloweenBoxCodename}},
		time.Date(1970, 1, 2, 4, 0, 1, 0, time.UTC).UnixMilli())
	if winner.HalloweenBoxCount != 1 || winner.HalloweenBoxDay != progression.HalloweenDayKey(nextDay) {
		t.Fatal("count across the reset", winner.HalloweenBoxDay, winner.HalloweenBoxCount)
	}
}

/*
================
TestHalloweenHelmetExchange

Sixty boxes buy the exact helmet row picked: too few answer the
no-box line with the bag untouched, the other sex's row is refused the
same way, and a fit grant consumes the price and lands the helmet.
================
*/
func TestHalloweenHelmetExchange(t *testing.T) {
	service := HalloweenHelmetServicePrefix + "ITEM_ETC_E071030_M_EGHEAD"

	// Too few boxes: the no-box line, nothing moved.
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 59)}
	rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
	frames, err := rt.ExchangeHalloweenHelmet(c, halloweenSoOkCodename, service)
	if err != nil || len(frames) != 1 ||
		!bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogSymbol(halloweenTalkNoBox)) {
		t.Fatal("short helmet exchange", frames, err)
	}
	if bagCount(c, progression.HalloweenBoxCodename) != 59 {
		t.Fatal("short helmet exchange touched the boxes", c.MissionInventory)
	}

	// The exact row at the price: sixty leave, the helmet lands.
	c = testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 60), halloweenBoxRow(21, 5)}
	rt, _ = halloweenRuntime(t, c, halloweenTestWindow)
	frames, err = rt.ExchangeHalloweenHelmet(c, halloweenSoOkCodename, service)
	if err != nil || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogSymbol(halloweenTalkGranted)) {
		t.Fatal("helmet exchange", frames, err)
	}
	if bagCount(c, progression.HalloweenBoxCodename) != 5 || bagCount(c, "ITEM_ETC_E071030_M_EGHEAD") != 1 {
		t.Fatal("helmet exchange bag", c.MissionInventory)
	}

	// A male character asking for the women's row is refused like a
	// short bag, and a female character is offered only her own rows.
	c = testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 60)}
	rt, _ = halloweenRuntime(t, c, halloweenTestWindow)
	frames, err = rt.ExchangeHalloweenHelmet(c, halloweenSoOkCodename, HalloweenHelmetServicePrefix+"ITEM_ETC_E071030_W_EGHEAD")
	if err != nil || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogSymbol(halloweenTalkNoBox)) {
		t.Fatal("cross-sex helmet exchange", frames, err)
	}
	female := int64(enterworld.GenderFemale)
	c.Gender = &female
	c.ModelCodename = "CHAR_CH_WOMAN_ADVENTURER"
	options := rt.HalloweenTalkOptions(halloweenSoOkCodename, c)
	if len(options) != 5 || options[1].Codename != HalloweenHelmetServicePrefix+"ITEM_ETC_E071030_W_EGHEAD" {
		t.Fatal("female helmet rows", options)
	}
}

/*
================
TestHalloweenHelmetSealBothSettings

The seal (its own switch, on after the event) refuses the helmet at
the trade table and the warehouse while on, and every door behaves
natively while off.
================
*/
func TestHalloweenHelmetSealBothSettings(t *testing.T) {
	c := testCharacter()
	helmetRow := enterworld.InventoryRow{
		Slot: 20, RefObjID: 23455, Codename: "ITEM_ETC_E071030_M_EGHEAD",
		TypeFlags: wire.PackTypeFlags(3, 1, 13, 0), StackCount: 1,
	}
	c.MissionInventory = []enterworld.InventoryRow{helmetRow}
	rt, _ := halloweenRuntime(t, c, halloweenTestWindow)
	t.Setenv(progression.EnvHalloweenHelmetSeal, "on")
	rt.HalloweenHelmetSeal = progression.HalloweenHelmetSealFromEnv()

	item, _ := bagOf(c).At(20)
	if rt.storableItem(item) {
		t.Fatal("sealed helmet entered the warehouse")
	}
	if _, code := rt.exchangeOfferable(c, 20); code == 0 {
		t.Fatal("sealed helmet reached the trade table")
	}

	rt.HalloweenHelmetSeal = progression.HalloweenHelmetSeal{}
	if !rt.storableItem(item) {
		t.Fatal("unsealed helmet refused by the warehouse")
	}
	if _, code := rt.exchangeOfferable(c, 20); code != 0 {
		t.Fatal("unsealed helmet refused by the trade table", code)
	}
}
