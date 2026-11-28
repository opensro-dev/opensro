/*
===========================================================================

tombbox_test.go - tests for tombbox.go

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tombbox"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const tombBoxCourage = "NPC_TQ_TREASUREBOX1"

/*
================
tombBoxRuntime

A runtime that resolves the four keys and box 1's potion and recipe,
with the switch as given.
================
*/
func tombBoxRuntime(c *enterworld.Character, on bool) *Runtime {
	items := halloweenTestItems()
	etc := func(refObjID uint32, codename string, maxStack float64) *enterworld.ItemRef {
		return &enterworld.ItemRef{
			RefObjID: refObjID, Codename: codename, TypeIDs: [4]int64{3, 3, 9, 0},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": maxStack}),
		}
	}
	for index, box := range tombbox.Boxes {
		items[box.Key] = etc(22626+uint32(index), box.Key, 50)
	}
	items["ITEM_ETC_ALL_POTION_05"] = etc(7, "ITEM_ETC_ALL_POTION_05", 50)
	rt, _ := newTestRuntime(c, items)
	rt.TombBoxes = on
	return rt
}

/*
================
tombKeyRow
================
*/
func tombKeyRow(slot int64) enterworld.InventoryRow {
	return enterworld.InventoryRow{
		Slot: slot, RefObjID: 22626, Codename: tombbox.Boxes[0].Key,
		TypeFlags: wire.PackTypeFlags(3, 3, 9, 0), StackCount: 1,
	}
}

/*
================
openCourage
================
*/
func openCourage(t *testing.T, rt *Runtime, c *enterworld.Character, draw uint32) []wire.Frame {
	t.Helper()
	rt.DropRoll = func() (uint32, error) { return draw, nil }
	frames, err := rt.openTombBox(testDivision, c, tombBoxCourage)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return frames
}

/*
================
TestTombBoxSwitchOffIsNative

Off, no box offers a row, an open is refused and no tomb monster drops a key.
================
*/
func TestTombBoxSwitchOffIsNative(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{tombKeyRow(20)}
	rt := tombBoxRuntime(c, false)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	if rt.tombBoxTalkOptions(tombBoxCourage) != nil {
		t.Fatal("a box offered its row with the switch off")
	}
	if _, err := rt.openTombBox(testDivision, c, tombBoxCourage); err == nil || bagCount(c, tombbox.Boxes[0].Key) != 1 {
		t.Fatal("a box opened with the switch off")
	}
	// The roll that wins Courage's key on (TestTombKeyDropsFromTombMonstersOnly).
	draws := 0
	rt.DropRoll = func() (uint32, error) {
		draws++
		if draws%2 == 1 {
			return 0, nil
		}
		return 5000, nil
	}
	archer := monster.Instance{Ref: monster.MonsterRef{Codename: "MOB_TQ_TOMBARCHER"}}
	if keys := rt.tombKeyDrops(archer, simulation.Spawn{}, c.Name, rt.Now()); len(keys) != 0 {
		t.Fatal("a key dropped with the switch off")
	}
}

/*
================
tombBoxBag

A bag holding the Courage key (when key is set) in its first slot and
filler in every other slot but the last free ones.
================
*/
func tombBoxBag(c *enterworld.Character, key bool, free int) {
	c.MissionInventory = nil
	first := int64(inventory.EquipmentSlotEnd)
	end := int64(inventory.BagEnd(c)) - int64(free)
	for slot := first; slot < end; slot++ {
		if key && slot == first {
			c.MissionInventory = append(c.MissionInventory, tombKeyRow(slot))
			continue
		}
		c.MissionInventory = append(c.MissionInventory, halloweenBoxRow(slot, 1))
	}
}

/*
================
TestTombBoxRefusesInNativeOrder

The talk choice checks the key before the room: no key answers the
no-key line even on a full bag; with the key, no free slot answers the
bag-full line and spends nothing, and exactly one free slot opens.
================
*/
func TestTombBoxRefusesInNativeOrder(t *testing.T) {
	c := testCharacter()
	rt := tombBoxRuntime(c, true)
	if options := rt.tombBoxTalkOptions(tombBoxCourage); len(options) != 1 || options[0].TitleSymbol != tombbox.TalkOption {
		t.Fatal("the box offers no open row", options)
	}

	tombBoxBag(c, false, 0)
	if !bytes.Equal(lastDialogue(t, openCourage(t, rt, c, 0)), wire.EncodeNpcDialogAnswer(tombbox.TalkNoKey)) {
		t.Fatal("no key on a full bag: want the no-key line")
	}

	tombBoxBag(c, true, 0)
	if freeBagSlots(c) != 0 {
		t.Fatalf("fixture leaves %d free slots, want 0", freeBagSlots(c))
	}
	if !bytes.Equal(lastDialogue(t, openCourage(t, rt, c, 0)), wire.EncodeNpcDialogAnswer(tombbox.TalkBagFull)) {
		t.Fatal("key, no free slot: want the bag-full line")
	}
	if bagCount(c, tombbox.Boxes[0].Key) != 1 {
		t.Fatal("a refusal spent the key")
	}

	tombBoxBag(c, true, 1)
	before := goldOf(c)
	openCourage(t, rt, c, 16383) // r 50.0: the gold outcome
	if bagCount(c, tombbox.Boxes[0].Key) != 0 || goldOf(c) != before+uint64(tombbox.Boxes[0].Gold) {
		t.Fatal("key and one free slot: the box did not open", goldOf(c), c.MissionInventory)
	}
}

/*
================
TestTombBoxOutcomesSpendTheKeyOnce

Each outcome spends exactly one key with its treasure: the gold purse,
the potion stack, and the damage, which leaves at least one HP.
================
*/
func TestTombBoxOutcomesSpendTheKeyOnce(t *testing.T) {
	box := tombbox.Boxes[0]

	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{tombKeyRow(20)}
	rt := tombBoxRuntime(c, true)
	before := goldOf(c)
	frames := openCourage(t, rt, c, 16383) // r 50.0: the gold band (39.28, 69.28]
	if goldOf(c) != before+uint64(box.Gold) || bagCount(c, box.Key) != 0 ||
		!bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(tombBoxGreeting(box))) {
		t.Fatal("gold outcome", goldOf(c), c.MissionInventory)
	}

	c = testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{tombKeyRow(20)}
	rt = tombBoxRuntime(c, true)
	openCourage(t, rt, c, 26214) // r 80.0: the potion band (69.28, 89.28]
	if bagCount(c, box.Potion) != int(box.PotionCount) || bagCount(c, box.Key) != 0 {
		t.Fatal("potion outcome", c.MissionInventory)
	}

	c = testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{tombKeyRow(20)}
	low := int64(1)
	c.CurrentHP = &low
	rt = tombBoxRuntime(c, true)
	frames = openCourage(t, rt, c, 0) // r 0: damage
	if *c.CurrentHP != 1 || bagCount(c, box.Key) != 0 ||
		!bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(tombbox.TalkDamage)) {
		t.Fatal("damage outcome at one HP", *c.CurrentHP, c.MissionInventory)
	}

	// At full HP the box takes its tenth of the maximum.
	c = testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{tombKeyRow(20)}
	rt = tombBoxRuntime(c, true)
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	maxHP, _, _, _ := keeperVitalsFrom(c, stats, nil)
	c.CurrentHP = &maxHP
	openCourage(t, rt, c, 0)
	if want := maxHP - maxHP*box.DamagePercent/100; *c.CurrentHP != want || want >= maxHP {
		t.Fatalf("damage at full HP: %d of %d, want %d", *c.CurrentHP, maxHP, want)
	}
}

/*
================
TestTombBoxItemOutcomeGrantsATopDegreePiece

r 99.5 is box 1's not-repairable normal item: the one degree-9 blade
the fixture resolves arrives at +4 with the key spent.
================
*/
func TestTombBoxItemOutcomeGrantsATopDegreePiece(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{tombKeyRow(20)}
	rt := tombBoxRuntime(c, true)
	const blade = "ITEM_CH_BLADE_09_C"
	rt.deps.(*enterworld.Deps).Items.(staticItemSource)[blade] = &enterworld.ItemRef{
		RefObjID: 4100, Codename: blade, TypeIDs: [4]int64{3, 1, 6, 3},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1, "itemClass": 27}),
	}
	openCourage(t, rt, c, 32604)
	var got *enterworld.InventoryRow
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Codename == blade {
			got = &c.MissionInventory[i]
		}
	}
	if got == nil || got.Plus != int64(tombbox.Boxes[0].ItemPlus[0]) || bagCount(c, tombbox.Boxes[0].Key) != 0 {
		t.Fatal("item outcome", c.MissionInventory)
	}
}

/*
================
TestTombKeyDropsFromTombMonstersOnly

With r = 0.005 on every pair of draws only Courage's 0.5 percent key wins;
a monster outside the tomb rolls nothing.
================
*/
func TestTombKeyDropsFromTombMonstersOnly(t *testing.T) {
	c := testCharacter()
	rt := tombBoxRuntime(c, true)
	draws := 0
	rt.DropRoll = func() (uint32, error) {
		draws++
		if draws%2 == 1 {
			return 0, nil
		}
		return 5000, nil
	}
	archer := monster.Instance{Ref: monster.MonsterRef{Codename: "MOB_TQ_TOMBARCHER"}}
	keys := rt.tombKeyDrops(archer, simulation.Spawn{}, c.Name, rt.Now())
	if len(keys) != 1 || keys[0].Codename != tombbox.Boxes[0].Key || draws != 2*len(tombbox.Boxes) {
		t.Fatalf("keys %v after %d draws", keys, draws)
	}
	draws = 0
	wolf := monster.Instance{Ref: monster.MonsterRef{Codename: "MOB_CH_WOLF"}}
	if keys := rt.tombKeyDrops(wolf, simulation.Spawn{}, c.Name, rt.Now()); len(keys) != 0 || draws != 0 {
		t.Fatal("an outdoor monster rolled a key")
	}
}
