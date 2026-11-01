/*
===========================================================================

stall_test.go - opening a stall, visiting it, buying from it and from afar

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
stallFixture

The exchange pair in a town; the owner also carries 20 potions in slot
13.
================
*/
func stallFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, map[string][]wire.Frame) {
	t.Helper()
	rt, owner, buyer, pushed := exchangeFixture(t)
	town := int64(0x62a8)
	for _, c := range []*enterworld.Character{owner, buyer} {
		if c.World == nil {
			c.World = &enterworld.CharacterWorld{}
		}
		if c.World.Spawn == nil {
			c.World.Spawn = &enterworld.WorldSpawn{}
		}
		c.World.Spawn.RegionID = &town
	}
	owner.MissionInventory = append(owner.MissionInventory, enterworld.InventoryRow{
		Slot: 13, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 20,
	})
	items := rt.deps.(*enterworld.Deps).Items.(staticItemSource)
	potion := *items["ITEM_ETC_HP_POTION_01"]
	potion.NativeFields = potion.NativeFields.With("canTrade", 1)
	items["ITEM_ETC_HP_POTION_01"] = &potion
	rt.StallCategories = map[[4]int64]uint32{{3, 3, 1, 1}: 40}
	return rt, owner, buyer, pushed
}

/*
================
stallEdit
================
*/
func stallEdit(kind uint8, body ...byte) []byte {
	return append([]byte{kind}, body...)
}

/*
================
TestStallSellsToAVisitor

The owner opens, puts 5 of 20 potions up for 300 gold and opens for
business; a visitor sees the offer and buys it: 5 potions land in the
visitor's first empty bag slot, 15 stay with the owner and 300 gold
changes hands.
================
*/
func TestStallSellsToAVisitor(t *testing.T) {
	rt, owner, buyer, pushed := stallFixture(t)
	title := wire.NewWriter(16).WStr("Potions").Payload()
	if out := rt.HandleStallCreate(testDivision, owner, title); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("create %+v", out.Frames)
	}
	add := stallEdit(wire.StallEditAdd, 0, 13, 5, 0, 0x2c, 1, 0, 0, 0, 0, 0, 0, 0)
	if out := rt.HandleStallEdit(testDivision, owner, add); len(out.Frames) != 1 || out.Frames[0].Payload[0] != 1 {
		t.Fatalf("add %+v", out.Frames)
	}
	if out := rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1)); !bytes.Equal(out.Frames[0].Payload, []byte{1, 5, 1, 1}) {
		t.Fatalf("open %+v", out.Frames)
	}
	if out := rt.HandleStallEdit(testDivision, owner, add); out.Frames[0].Payload[0] != 2 {
		t.Fatal("an open stall took a new offer")
	}
	visit := rt.HandleStallVisit(testDivision, buyer, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(owner)).Payload())
	// The visitor's client decodes the listed potion through its own
	// reference table, so the potion's reference comes first.
	if len(visit.Frames) != 2 || !referencesName(visit.Frames[0], stallSlotItem(t, owner, 13)) ||
		visit.Frames[1].Opcode != wire.OpStallVisitResult || visit.Frames[1].Payload[0] != 1 {
		t.Fatalf("visit %+v", visit.Frames)
	}
	lastFrame(t, pushed[owner.Name], wire.OpStallEvent)

	listed := rt.HandleStallNetworkSearch(testDivision, buyer, []byte{0, 0, 40, 0, 0, 0, 0})
	// The page lists another player's potion: its reference comes first.
	if len(listed.Frames) != 2 || !referencesName(listed.Frames[0], stallSlotItem(t, owner, 13)) {
		t.Fatalf("network search frames %+v", listed.Frames)
	}
	if p := listed.Frames[1].Payload; p[0] != 1 || p[1] != 1 || p[2] != 1 {
		t.Fatalf("network search % X", p)
	}
	// A potion has no degree: a third-degree search does not list it.
	if p := rt.HandleStallNetworkSearch(testDivision, buyer, []byte{0, 0, 40, 0, 0, 0, 3}).Frames[0].Payload; p[1] != 0 {
		t.Fatalf("degree search % X", p)
	}

	if out := rt.HandleStallBuy(testDivision, buyer, []byte{0}); !bytes.Equal(out.Frames[0].Payload, []byte{1, 0}) {
		t.Fatalf("buy %+v", out.Frames)
	}
	if goldOf(buyer) != 4700 || goldOf(owner) != 5300 {
		t.Fatalf("gold %d / %d", goldOf(buyer), goldOf(owner))
	}
	if len(buyer.MissionInventory) != 1 || buyer.MissionInventory[0].Slot != 13 || buyer.MissionInventory[0].StackCount != 5 {
		t.Fatalf("buyer bag %+v", buyer.MissionInventory)
	}
	for _, row := range owner.MissionInventory {
		if row.Slot == 13 && row.StackCount != 15 {
			t.Fatalf("owner kept %d potions", row.StackCount)
		}
	}
	sold := lastFrame(t, pushed[owner.Name], wire.OpStallEvent)
	if sold.Payload[0] != wire.StallEventSold || sold.Payload[1] != 0 {
		t.Fatalf("sale event % X", sold.Payload)
	}
}

/*
================
stallAddOfferAt

An 0x71A8 kind 2 add of five potions at one unit price; wide writes the
port-only u64 price, false the native u32.
================
*/
func stallAddOfferAt(slot, bagSlot uint8, price uint64, wide bool) []byte {
	w := wire.NewWriter(24).U8(wire.StallEditAdd).U8(slot).U8(bagSlot).U16(5)
	if wide {
		w.U64(price)
	} else {
		w.U32(uint32(price))
	}
	return w.U32(0).U8(0).Payload()
}

/*
================
stallAddOffer

The fixture's slot-13 potions offered in the first stall slot.
================
*/
func stallAddOffer(price uint64, wide bool) []byte {
	return stallAddOfferAt(0, 13, price, wide)
}

/*
================
stallModifyOffer

An 0x71A8 kind 1 modify of the first offer's price; wide writes the
port-only u64 price, false the native u32.
================
*/
func stallModifyOffer(price uint64, wide bool) []byte {
	w := wire.NewWriter(16).U8(wire.StallEditModify).U8(0).U16(5)
	if wide {
		w.U64(price)
	} else {
		w.U32(uint32(price))
	}
	return w.U8(0).Payload()
}

/*
================
stallU64

The little-endian u64 at at.
================
*/
func stallU64(p []byte, at int) uint64 {
	return uint64(p[at]) | uint64(p[at+1])<<8 | uint64(p[at+2])<<16 | uint64(p[at+3])<<24 |
		uint64(p[at+4])<<32 | uint64(p[at+5])<<40 | uint64(p[at+6])<<48 | uint64(p[at+7])<<56
}

/*
================
TestStallOfferPriceCaps

Off, the native 1,000,000,000 price is the ceiling and the native u32
offer wire is used. On, a 10,000,000,000 price is accepted and stored, and
one above it is refused.
================
*/
func TestStallOfferPriceCaps(t *testing.T) {
	for _, tc := range []struct {
		name    string
		on      bool
		wide    bool
		price   uint64
		refused bool
	}{
		{"off native ceiling", false, false, NativeStallPriceLimit, false},
		{"off above native", false, false, NativeStallPriceLimit + 1, true},
		{"on native u32 wire", true, false, NativeStallPriceLimit, false},
		{"on ten billion", true, true, RaisedStallPriceLimit, false},
		{"on above raised", true, true, RaisedStallPriceLimit + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, owner, _, _ := stallFixture(t)
			rt.RaisedGoldCaps = tc.on
			rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
			out := rt.HandleStallEdit(testDivision, owner, stallAddOffer(tc.price, tc.wide))
			p := out.Frames[0].Payload
			if tc.refused {
				if len(p) != 2 || p[0] != 2 || p[1] != wire.StallErrBadPrice {
					t.Fatalf("price %d answered % X, want a bad-price refusal", tc.price, p)
				}
				if s, _ := rt.Stalls.Get(testDivision, owner.Name); s.Slots[0] != nil {
					t.Fatalf("refused price %d was stored", tc.price)
				}
				return
			}
			if p[0] != 1 {
				t.Fatalf("price %d answered % X, want success", tc.price, p)
			}
			s, _ := rt.Stalls.Get(testDivision, owner.Name)
			if s.Slots[0] == nil || s.Slots[0].Price != tc.price {
				t.Fatalf("stored offer %+v, want price %d", s.Slots[0], tc.price)
			}
		})
	}
}

/*
================
TestStallModifyAnswerUsesShardWidth

A raised shard answers every modify with the u64 price even when the
request itself was the native u32 one; a native shard keeps u32. The
answer is broadcast, so a mismatched width would break every visitor's
decode as well as the owner's.
================
*/
func TestStallModifyAnswerUsesShardWidth(t *testing.T) {
	for _, tc := range []struct {
		name        string
		on          bool
		requestWide bool
		answerWide  bool
		price       uint64
	}{
		{"off native", false, false, false, NativeStallPriceLimit},
		{"on native request", true, false, true, NativeStallPriceLimit},
		{"on wide request", true, true, true, RaisedStallPriceLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, owner, _, _ := stallFixture(t)
			rt.RaisedGoldCaps = tc.on
			rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
			if p := rt.HandleStallEdit(testDivision, owner, stallAddOffer(NativeStallPriceLimit, tc.on)).Frames[0].Payload; p[0] != 1 {
				t.Fatalf("add % X", p)
			}
			price := tc.price
			out := rt.HandleStallEdit(testDivision, owner, stallModifyOffer(price, tc.requestWide))
			p := out.Frames[0].Payload
			priceWidth := 4
			if tc.answerWide {
				priceWidth = 8
			}
			if p[0] != 1 || p[1] != wire.StallEditModify || len(p) != 6+priceWidth {
				t.Fatalf("modify answered % X, want success and a %d-byte price", p, priceWidth)
			}
			got := uint64(0)
			if tc.answerWide {
				got = stallU64(p, 5)
			} else {
				got = uint64(p[5]) | uint64(p[6])<<8 | uint64(p[7])<<16 | uint64(p[8])<<24
			}
			if got != price {
				t.Fatalf("answer price %d, want %d (% X)", got, price, p)
			}
			if s, _ := rt.Stalls.Get(testDivision, owner.Name); s.Slots[0] == nil || s.Slots[0].Price != price {
				t.Fatalf("stored offer %+v, want price %d", s.Slots[0], price)
			}
		})
	}
}

/*
================
TestStallTenBillionSalePaysCommission

A raised shard's networked ten-billion offer sells end to end: the buyer
pays the price, the owner gains it less the network commission, and the
sold notice carries the remaining offer in the raised u64 width.
================
*/
func TestStallTenBillionSalePaysCommission(t *testing.T) {
	rt, owner, buyer, pushed := stallFixture(t)
	rt.RaisedGoldCaps = true
	for _, row := range owner.MissionInventory {
		if row.Slot == 13 {
			second := row
			second.Slot = 14
			owner.MissionInventory = append(owner.MissionInventory, second)
			break
		}
	}
	rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
	for _, offer := range []struct{ slot, bagSlot uint8 }{{0, 13}, {1, 14}} {
		if p := rt.HandleStallEdit(testDivision, owner,
			stallAddOfferAt(offer.slot, offer.bagSlot, RaisedStallPriceLimit, true)).Frames[0].Payload; p[0] != 1 {
			t.Fatalf("add slot %d % X", offer.slot, p)
		}
	}
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1))
	setGold(buyer, RaisedStallPriceLimit)
	setGold(owner, 0)
	s, _ := rt.Stalls.Get(testDivision, owner.Name)
	gid := enterworld.ObjectIDForCharacter(owner)
	buy := wire.NewWriter(24).U32(gid).U8(0).U64(RaisedStallPriceLimit).U16(5).U8(0).U64(s.Slots[0].Serial).Payload()
	if out := rt.HandleStallNetworkBuy(testDivision, buyer, buy); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("network buy %+v", out.Frames)
	}
	if goldOf(buyer) != 0 || goldOf(owner) != RaisedStallPriceLimit-stallCommission(RaisedStallPriceLimit) {
		t.Fatalf("gold buyer %d owner %d, want 0/%d", goldOf(buyer), goldOf(owner),
			RaisedStallPriceLimit-stallCommission(RaisedStallPriceLimit))
	}
	sold := lastFrame(t, pushed[owner.Name], wire.OpStallEvent)
	if sold.Payload[0] != wire.StallEventNetworkSold || sold.Payload[1] != 0 {
		t.Fatalf("sold event % X", sold.Payload)
	}
	// The remaining slot-1 offer is the notice's last one, so its u64 price
	// sits just before the 0xFF end marker.
	if got := stallU64(sold.Payload, len(sold.Payload)-9); got != RaisedStallPriceLimit {
		t.Fatalf("sold notice price %d, want %d (% X)", got, RaisedStallPriceLimit, sold.Payload)
	}
}

/*
================
TestStallNetworkListsTenBillion

With the switch on, the network row carries the full ten-billion price in
its native u64 field.
================
*/
func TestStallNetworkListsTenBillion(t *testing.T) {
	rt, owner, buyer, _ := stallFixture(t)
	rt.RaisedGoldCaps = true
	rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
	if p := rt.HandleStallEdit(testDivision, owner, stallAddOffer(RaisedStallPriceLimit, true)).Frames[0].Payload; p[0] != 1 {
		t.Fatalf("add % X", p)
	}
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1))
	listed := rt.HandleStallNetworkSearch(testDivision, buyer, []byte{0, 0, 40, 0, 0, 0, 0})
	p := listed.Frames[len(listed.Frames)-1].Payload
	// The row ends with the u64 price and then its u64 serial.
	priceAt := len(p) - 16
	price := uint64(p[priceAt]) | uint64(p[priceAt+1])<<8 | uint64(p[priceAt+2])<<16 |
		uint64(p[priceAt+3])<<24 | uint64(p[priceAt+4])<<32 | uint64(p[priceAt+5])<<40 |
		uint64(p[priceAt+6])<<48 | uint64(p[priceAt+7])<<56
	if price != RaisedStallPriceLimit {
		t.Fatalf("network price %d, want %d (frame % X)", price, RaisedStallPriceLimit, p)
	}
}

/*
================
TestStallNetworkSaleTakesCommission

A networked offer bought from afar pays the owner its price less 1%.
================
*/
func TestStallNetworkSaleTakesCommission(t *testing.T) {
	rt, owner, buyer, _ := stallFixture(t)
	rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditAdd, 0, 13, 20, 0, 0xe8, 3, 0, 0, 0, 0, 0, 0, 0))
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1))
	s, _ := rt.Stalls.Get(testDivision, owner.Name)
	gid := enterworld.ObjectIDForCharacter(owner)
	stale := wire.NewWriter(24).U32(gid).U8(0).U64(1000).U16(20).U8(0).U64(s.Slots[0].Serial + 1).Payload()
	if out := rt.HandleStallNetworkBuy(testDivision, buyer, stale); !bytes.Equal(out.Frames[0].Payload, []byte{2, wire.StallErrNetworkStale}) {
		t.Fatalf("a stale row answered %+v", out.Frames)
	}
	buy := wire.NewWriter(24).U32(gid).U8(0).U64(1000).U16(20).U8(0).U64(s.Slots[0].Serial).Payload()
	if out := rt.HandleStallNetworkBuy(testDivision, buyer, buy); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("network buy %+v", out.Frames)
	}
	if goldOf(buyer) != 4000 || goldOf(owner) != 5990 {
		t.Fatalf("gold %d / %d", goldOf(buyer), goldOf(owner))
	}
}

/*
================
TestStallNetworkDegreeIsTheItemsDegree

A non-equipment item's degree is its class (reference server): a class-3 expendable
lists under a third-degree search, not a first-degree one.
================
*/
func TestStallNetworkDegreeIsTheItemsDegree(t *testing.T) {
	rt, owner, buyer, _ := stallFixture(t)
	items := rt.deps.(*enterworld.Deps).Items.(staticItemSource)
	stone := *items["ITEM_ETC_HP_POTION_01"]
	stone.NativeFields = stone.NativeFields.With("itemClass", 3)
	items["ITEM_ETC_HP_POTION_01"] = &stone
	rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Stones").Payload())
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditAdd, 0, 13, 5, 0, 0x2c, 1, 0, 0, 0, 0, 0, 0, 0))
	rt.HandleStallEdit(testDivision, owner, stallEdit(wire.StallEditOpen, 1, 1))
	for _, c := range []struct {
		degree, rows byte
	}{{3, 1}, {1, 0}} {
		p := rt.HandleStallNetworkSearch(testDivision, buyer, []byte{0, 0, 40, 0, 0, 0, c.degree})
		if got := p.Frames[len(p.Frames)-1].Payload; got[1] != c.rows {
			t.Errorf("degree %d search % X, want %d rows", c.degree, got, c.rows)
		}
	}
}

/*
================
TestStallCreateStopsAWalkingHost

BR thread "Stall Jackson": a host opening a stall on the move kept
walking with the stall up. The original admits the stall and stops the
host first: the walk settles at the live point, the stop (B2F5) reaches
the host and its viewers ahead of the create answer, and the pending
attack command is gone.
================
*/
func TestStallCreateStopsAWalkingHost(t *testing.T) {
	rt, owner, _, _ := stallFixture(t)
	key := simulation.WorldKey(testDivision, owner.Name)
	now := rt.Now().UnixMilli()
	before := rt.Worlds.Update(key,
		func() simulation.WorldState { return simulation.SeedWorldState(owner) },
		func(w *simulation.WorldState) {
			from := w.Spawn
			w.Spawn.X += 200
			w.MoveSegment = &simulation.MoveSegment{From: from, StartedAtMs: now - 2000, ArrivesAtMs: now + 2000}
		})
	want := before.LiveSpawnAt(now)
	rt.setCombatIntent(basicAttackIntent{DivisionID: testDivision, CharacterName: owner.Name, TargetGid: 7})

	out := rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
	if len(out.Frames) < 2 || out.Frames[0].Opcode != wire.OpObjectSourceCorrection || out.Frames[1].Opcode != wire.OpStallCreateResult ||
		!bytes.Equal(out.Frames[1].Payload, []byte{1}) {
		t.Fatalf("create frames %+v, want the stop then a successful create", out.Frames)
	}
	for name, frames := range map[string][]wire.Frame{"host": out.Frames, "viewers": out.Broadcast} {
		stop, ok := findCorrection(frames)
		if !ok || stop.Gid != enterworld.ObjectIDForCharacter(owner) || stop.Position.X != float32(want.X) || stop.Position.Z != float32(want.Z) {
			t.Fatalf("%s stop %+v (%v), want %+v", name, stop, ok, want)
		}
	}
	after := rt.Worlds.Snapshot(key, func() simulation.WorldState { panic("lost world") })
	if after.MovingAt(rt.Now().UnixMilli()) || after.Spawn.X != want.X || after.Spawn.Z != want.Z {
		t.Fatalf("host still walking with the stall up: %+v, want standing at %+v", after, want)
	}
	rt.basicAttackIntentsMu.Lock()
	_, pending := rt.basicAttackIntents[key]
	rt.basicAttackIntentsMu.Unlock()
	if pending {
		t.Fatal("the host's pending attack survived the stall opening")
	}
}

/*
================
TestStallCreateLeavesAStandingHostAlone

A standing host gets no stop frame: the create answer comes first, as
before.
================
*/
func TestStallCreateLeavesAStandingHostAlone(t *testing.T) {
	rt, owner, _, _ := stallFixture(t)
	out := rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload())
	if out.Frames[0].Opcode != wire.OpStallCreateResult || len(out.Broadcast) != 0 {
		t.Fatalf("create frames %+v broadcast %+v, want only the create answer", out.Frames, out.Broadcast)
	}
}
