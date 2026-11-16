/*
===========================================================================

sook_christmas_test.go - So-Ok's Christmas Gift: the box drop, Santa
So-Ok's exchange, the box cleanup and the event groups

===========================================================================
*/
package action

import (
	"bytes"
	"math"
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
sookChristmasRuntime

The Halloween fixture's items plus every 2006 gift; the Christmas switch
on (or off) in mode so_ok_2006 over the Halloween tests' 1970 window,
which covers the fixture clock.
================
*/
func sookChristmasRuntime(t *testing.T, c *enterworld.Character, on bool) *Runtime {
	t.Helper()
	items := halloweenTestItems()
	for i, gift := range progression.SoOkChristmasDefaultGifts() {
		items[gift.Codename] = &enterworld.ItemRef{
			RefObjID: uint32(40000 + i), Codename: gift.Codename, TypeIDs: [4]int64{3, 3, 13, 1},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50}),
		}
	}
	rt, _ := newTestRuntime(c, items)
	setting := "off"
	if on {
		setting = "on"
	}
	t.Setenv(progression.EnvChristmasEvent, setting)
	t.Setenv(progression.EnvChristmasEventWindow, halloweenTestWindow)
	t.Setenv(progression.EnvChristmasEventMode, string(progression.ChristmasModeSoOk2006))
	rt.Christmas = progression.ChristmasEventFromEnv()
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()
	return rt
}

// sookChristmasMob is an ordinary level-1 monster, the fixture
// character's own level, so the native admission admits its drops.
var sookChristmasMob = monster.Instance{Ref: monster.MonsterRef{Codename: "MOB_CH_MANGNYANG", Level: 1, MaxHP: 50}}

/*
================
TestSoOkChristmasBoxDropRules

One roll per kill at the configured chance (1.5 % of 32768 is 491.52:
draw 491 pays, 492 does not), then the native admission; no box from a
unique, a GM-conjured kill, a monster far below the hunter, or with the
event off. The winter event's runtime field stays off in this mode.
================
*/
func TestSoOkChristmasBoxDropRules(t *testing.T) {
	c := testCharacter()
	rt := sookChristmasRuntime(t, c, true)
	if rt.Christmas.Enabled() {
		t.Fatal("so_ok_2006 mode: the winter event must stay off")
	}
	now := rt.Now()
	drop := func(rt *Runtime, mob monster.Instance, draws ...uint32) bool {
		t.Helper()
		rt.DropRoll = dropRollSequence(draws...)
		box, ok := rt.sookChristmasBoxDrop(c, mob, simulation.Spawn{}, now)
		if ok && box.Codename != progression.SoOkChristmasBoxCodename {
			t.Fatalf("dropped %s", box.Codename)
		}
		return ok
	}
	if !drop(rt, sookChristmasMob, 491, 0) {
		t.Fatal("draw 491 is inside 1.5 %")
	}
	if drop(rt, sookChristmasMob, 492, 0) {
		t.Fatal("draw 492 is outside 1.5 %")
	}
	unique := sookChristmasMob
	unique.Ref.MonsterType = 3
	if drop(rt, unique, 0, 0) {
		t.Fatal("a unique paid a box")
	}
	conjured := sookChristmasMob
	conjured.GMSpawned = true
	if drop(rt, conjured, 0, 0) {
		t.Fatal("a GM-conjured kill paid a box")
	}
	high := testCharacter()
	level := int64(90)
	high.Level = &level
	rt.DropRoll = dropRollSequence(0, 100)
	if _, ok := rt.sookChristmasBoxDrop(high, sookChristmasMob, simulation.Spawn{}, now); ok {
		t.Fatal("a level-90 hunter looted a box from a level-1 monster past the native gap")
	}
	t.Setenv(progression.EnvSoOkChristmasBoxChance, "100")
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()
	if !drop(rt, sookChristmasMob, 32767, 0) {
		t.Fatal("a configured 100 % pays every kill")
	}
	if drop(sookChristmasRuntime(t, c, false), sookChristmasMob, 0, 0) {
		t.Fatal("event off: a box dropped")
	}
}

/*
================
TestSoOkChristmasBoxIgnoresTheDropRates

The beta drop rate scales a kill's ordinary passes, never the event's
box: a kill whose every roll succeeds at rate 20 leaves exactly one box.
================
*/
func TestSoOkChristmasBoxIgnoresTheDropRates(t *testing.T) {
	c := testCharacter()
	rt := sookChristmasRuntime(t, c, true)
	rt.DropPassRate = 20
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	boxes := 0
	for _, item := range rt.planMonsterKillLoot(c, sookChristmasMob, monster.Pose{}, rt.Now().UnixMilli()) {
		if item.Codename == progression.SoOkChristmasBoxCodename {
			boxes++
			if item.OwnerJID != enterworld.ObjectIDForCharacter(c) {
				t.Fatal("the box is not the killer's")
			}
		}
	}
	if boxes != 1 {
		t.Fatalf("a kill at drop rate 20 left %d boxes, want 1", boxes)
	}
}

/*
================
TestSoOkChristmasExchangeAtSantaSoOk

The 2006 row only while the event runs and only at So-ok; no box
answers the no-box line; a bag too full for the gift answers the
bag-full line and keeps every box; three boxes buy three gifts.
================
*/
func TestSoOkChristmasExchangeAtSantaSoOk(t *testing.T) {
	c := testCharacter()
	if options := sookChristmasRuntime(t, c, false).SoOkChristmasTalkOptions(halloweenSoOkCodename, c); len(options) != 0 {
		t.Fatal("options offered with the event off")
	}
	rt := sookChristmasRuntime(t, c, true)
	if options := rt.ChristmasTalkOptions(halloweenSoOkCodename, c); len(options) != 0 {
		t.Fatal("the winter event's socks row is offered in so_ok_2006 mode")
	}
	options := rt.SoOkChristmasTalkOptions(halloweenSoOkCodename, c)
	if len(options) != 1 || options[0].Codename != SoOkChristmasExchangeService ||
		options[0].TitleSymbol != sookChristmasTalkOption || options[0].PromptSymbol != sookChristmasTalkPrompt {
		t.Fatal("talk options", options)
	}
	if options := rt.SoOkChristmasTalkOptions("NPC_CH_SMITH", c); len(options) != 0 {
		t.Fatal("options offered at another NPC")
	}

	c.MissionInventory = nil
	frames, err := rt.ExchangeSoOkChristmasBoxes(c, halloweenSoOkCodename)
	if err != nil || len(frames) != 1 || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(sookChristmasTalkNoBox)) {
		t.Fatal("no-box exchange", frames, err)
	}

	potion := halloweenTestItems()["ITEM_ETC_HP_SPOTION_01"]
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.BagEnd(c)); slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: slot, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 50,
		})
	}
	c.MissionInventory[0] = halloweenBoxRow(c.MissionInventory[0].Slot, 2)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	frames, err = rt.ExchangeSoOkChristmasBoxes(c, halloweenSoOkCodename)
	if err != nil || len(frames) != 1 || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(sookChristmasTalkBagFull)) {
		t.Fatal("full-bag exchange", frames, err)
	}
	if bagCount(c, progression.SoOkChristmasBoxCodename) != 2 {
		t.Fatal("a full bag consumed a box", c.MissionInventory)
	}

	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 3)}
	frames, err = rt.ExchangeSoOkChristmasBoxes(c, halloweenSoOkCodename)
	if err != nil || !bytes.Equal(lastDialogue(t, frames), wire.EncodeNpcDialogAnswer(sookChristmasTalkGranted)) {
		t.Fatal("granted exchange", frames, err)
	}
	// Roll 0 lands on the table's first row, one item per box.
	first := progression.SoOkChristmasDefaultGifts()[0].Codename
	if bagCount(c, progression.SoOkChristmasBoxCodename) != 0 || bagCount(c, first) != 3 {
		t.Fatal("three boxes, three gifts", c.MissionInventory)
	}
}

/*
================
TestSoOkChristmasGiftsFollowTheirWeights

Every one of the roll's 32768 draws, once: each gift's share is its
weight's, to the roll's own resolution; a configured table replaces the
default one.
================
*/
func TestSoOkChristmasGiftsFollowTheirWeights(t *testing.T) {
	rt := sookChristmasRuntime(t, testCharacter(), true)
	gifts := rt.SoOkChristmas.Gifts()
	counts := map[string]int{}
	draw := uint32(0)
	rt.DropRoll = func() (uint32, error) { d := draw; draw++; return d, nil }
	for range sookChristmasChanceScale {
		gift, err := rt.rollExchangeGift(gifts)
		if err != nil {
			t.Fatal(err)
		}
		counts[gift.Codename]++
	}
	total := 0.0
	for _, gift := range gifts {
		total += gift.Weight
	}
	for _, gift := range gifts {
		share := float64(counts[gift.Codename]) / sookChristmasChanceScale
		if math.Abs(share-gift.Weight/total) > 0.001 {
			t.Fatalf("%s: share %.4f, weight %.4f", gift.Codename, share, gift.Weight/total)
		}
	}

	t.Setenv(progression.EnvSoOkChristmasGifts, "ITEM_ETC_E051123_HIT_SCROLL=1")
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 2)}
	rt.DropRoll = func() (uint32, error) { return 32767, nil }
	if _, err := rt.ExchangeSoOkChristmasBoxes(c, halloweenSoOkCodename); err != nil || bagCount(c, "ITEM_ETC_E051123_HIT_SCROLL") != 2 {
		t.Fatal("the configured table", c.MissionInventory, err)
	}
}

/*
================
TestSoOkChristmasBoxesStayWhileEitherEventRuns

The box is Halloween's token too. Inside the 2006 window the entry
keeps every box even though Halloween's window has long ended; after the
2006 window the entry takes them. In winter mode the Christmas window
says nothing about boxes.
================
*/
func TestSoOkChristmasBoxesStayWhileEitherEventRuns(t *testing.T) {
	character := func() *enterworld.Character {
		c := testCharacter()
		c.MissionInventory = []enterworld.InventoryRow{halloweenBoxRow(20, 4)}
		return c
	}
	t.Setenv(progression.EnvHalloweenEvent, "on")
	t.Setenv(progression.EnvHalloweenEventWindow, "1969-10-30T00:00:00Z/1969-11-02T00:00:00Z")
	halloween := progression.HalloweenEventFromEnv()

	inside := character()
	rt := sookChristmasRuntime(t, inside, true)
	rt.Halloween = halloween
	if rt.RemoveExpiredEventItems(inside) || bagCount(inside, progression.SoOkChristmasBoxCodename) != 4 {
		t.Fatal("inside the 2006 window the boxes must stay")
	}

	after := character()
	rt = sookChristmasRuntime(t, after, true)
	t.Setenv(progression.EnvChristmasEventWindow, "1969-12-26T00:00:00Z/1969-12-31T00:00:00Z")
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()
	if !rt.RemoveExpiredEventItems(after) || bagCount(after, progression.SoOkChristmasBoxCodename) != 0 {
		t.Fatal("after the 2006 window the boxes must leave")
	}

	winter := character()
	rt = sookChristmasRuntime(t, winter, true)
	t.Setenv(progression.EnvChristmasEventMode, string(progression.ChristmasModeWinter))
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()
	if rt.RemoveExpiredEventItems(winter) || bagCount(winter, progression.SoOkChristmasBoxCodename) != 4 {
		t.Fatal("winter mode: an ended Christmas window must not take Halloween's boxes")
	}
}

/*
================
TestSoOkChristmasEventGroups

Inside the window an entry lists the decoration and the town carols
group, and the native snow group only during a snowfall (the winter
event's timetable: the even UTC hours); outside it, nothing. Each edge goes out once.
================
*/
func TestSoOkChristmasEventGroups(t *testing.T) {
	rt, clock := waveTestRuntime(t, false)
	start := time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC)
	t.Setenv(progression.EnvChristmasEvent, "on")
	t.Setenv(progression.EnvChristmasEventMode, string(progression.ChristmasModeSoOk2006))
	t.Setenv(progression.EnvChristmasEventWindow, "2026-12-26T00:00:00Z/2027-01-09T00:00:00Z")
	rt.Christmas = progression.ChristmasEventFromEnv()
	rt.SoOkChristmas = progression.SoOkChristmasFromEnv()

	at := func(offset time.Duration, want ...uint32) {
		t.Helper()
		*clock = start.Add(offset)
		got := rt.EntryEventGroups()
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("at +%v: groups %v, want %v", offset, got, want)
		}
	}
	at(-time.Minute)
	at(5*time.Minute, ChristmasEventGroup, ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup)
	at(59*time.Minute+59*time.Second, ChristmasEventGroup, ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup)
	at(time.Hour, ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup)
	at(4*time.Hour+30*time.Minute, ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup)
	at(26*time.Hour+3*time.Minute, ChristmasEventGroup, ChristmasDecorationEventGroup, ChristmasTownCarolsEventGroup)
	at(14 * 24 * time.Hour)

	// The snow edges: one 0x3347 for group 1 at 01:00 and again at 02:00.
	*clock = start.Add(10 * time.Minute)
	rt.advanceEventGroups(0)
	edge := func(at time.Time, on bool) {
		t.Helper()
		*clock = at
		out := rt.advanceEventGroups(0)
		if len(out) != 1 || len(out[0].Frames) != 1 || !bytes.Equal(out[0].Frames[0].Payload, EventGroupStatePayload(ChristmasEventGroup, on)) {
			t.Fatalf("at %v: %+v", at, out)
		}
		if again := rt.advanceEventGroups(0); len(again) != 0 {
			t.Fatalf("at %v: the change went out twice", at)
		}
	}
	edge(start.Add(time.Hour), false)
	edge(start.Add(2*time.Hour), true)
}

/*
================
TestEverySoOkChristmasItemIsInShippedData

The box and every default gift by codename, in the shipped data, as the
2006 talk rows are.
================
*/
func TestEverySoOkChristmasItemIsInShippedData(t *testing.T) {
	clientdata.RequireGameData(t)
	items := enterworld.NewTextdataItems(clientdata.RetailTextdataDir(t))
	codenames := []string{progression.SoOkChristmasBoxCodename}
	for _, gift := range progression.SoOkChristmasDefaultGifts() {
		codenames = append(codenames, gift.Codename)
	}
	for _, codename := range codenames {
		if ref, ok := items.ItemRefByCodename(codename); !ok || ref == nil {
			t.Fatalf("%s is not in shipped data", codename)
		}
	}
}
