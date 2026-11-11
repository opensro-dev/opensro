/*
===========================================================================

tradestars_test.go - a scene going live repeats a loaded transport's level

Every loading scene builds a new local player whose trade level starts at
0, so game-ready tells it again while goods ride the transport.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSceneRepeatsTradeLevelOnlyWithGoodsAboard
================
*/
func TestSceneRepeatsTradeLevelOnlyWithGoodsAboard(t *testing.T) {
	rt, c := traderFixture(t, false)
	if _, loaded := rt.sceneTradeStars(c); loaded {
		t.Fatal("an empty transport repeated a trade level")
	}
	row := c.MissionInventory[1]
	row.Slot = 0
	row.StackCount = 40
	c.ActiveCOS.Container.Rows = []domain.InventoryRow{row}
	frame, loaded := rt.sceneTradeStars(c)
	if !loaded || frame.Opcode != wire.OpTradeStarCount || len(frame.Payload) != 1 {
		t.Fatalf("loaded transport frame %+v (%v), want one 0x352D byte", frame, loaded)
	}
	if want := caravan.DifficultyTier(rt.caravanCargoValue(c)); frame.Payload[0] != want {
		t.Fatalf("repeated level %d, want the cargo tier %d", frame.Payload[0], want)
	}
	c.ActiveCOS.Summoned = false
	if _, loaded := rt.sceneTradeStars(c); loaded {
		t.Fatal("a dismissed transport repeated a trade level")
	}
	if _, loaded := rt.sceneTradeStars(nil); loaded {
		t.Fatal("no character repeated a trade level")
	}
}

/*
================
TestTradeProgressRidesWithTheStarAnswer

#3163: under SRO_TRADE_TIERS the port-only progress answer carries
the same stars as 0x352D and the gold missing from the next star,
right after the star answer of a committed cargo move; with the flag
off nothing extra is published.
================
*/
func TestTradeProgressRidesWithTheStarAnswer(t *testing.T) {
	rt, c := traderFixture(t, false)
	if _, ok := rt.tradeProgressFrame(c); ok {
		t.Fatal("progress published with SRO_TRADE_TIERS off")
	}
	move := OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: []byte{1}}}}
	out := rt.withTradeStars(testDivision, c, wire.MoveTypeCosShopBuy, move)
	if len(out.Frames) != 2 || out.Frames[1].Opcode != wire.OpTradeStarCount {
		t.Fatalf("flag-off answer %+v, want the move and its star only", out.Frames)
	}
	rt.TradeTiers = true
	// One stack of the fixture's goods (40 x 101 gold) rides a 4-slot
	// transport: a quarter of its brim-full value is 2 stars, and
	// 2/5 of 16160 less the 4040 aboard is 2424 gold short of 3.
	row := c.MissionInventory[1]
	row.Slot = 0
	row.StackCount = 40
	c.ActiveCOS.Container.Rows = []domain.InventoryRow{row}
	frame, ok := rt.tradeProgressFrame(c)
	if !ok || frame.Opcode != wire.OpTradeProgress || len(frame.Payload) != 5 {
		t.Fatalf("progress frame %+v (%v), want the 5-byte port-only body", frame, ok)
	}
	if frame.Payload[0] != 2 || binary.LittleEndian.Uint32(frame.Payload[1:]) != 2424 {
		t.Fatalf("progress body %v, want 2 stars and 2424 gold", frame.Payload)
	}
	if star := rt.tradeStarFrame(c); star.Payload[0] != frame.Payload[0] {
		t.Fatalf("star answer %d disagrees with progress %d", star.Payload[0], frame.Payload[0])
	}
	out = rt.withTradeStars(testDivision, c, wire.MoveTypeCosShopBuy, move)
	if len(out.Frames) != 3 || out.Frames[1].Opcode != wire.OpTradeStarCount || out.Frames[2].Opcode != wire.OpTradeProgress {
		t.Fatalf("move answer %+v, want the star answer then its progress", out.Frames)
	}
	// Empty again: zero stars, one gold of goods short of the first.
	c.ActiveCOS.Container.Rows = nil
	frame, ok = rt.tradeProgressFrame(c)
	if !ok || frame.Payload[0] != 0 || binary.LittleEndian.Uint32(frame.Payload[1:]) != 1 {
		t.Fatalf("empty progress %+v (%v), want 0 stars and a gap of 1", frame, ok)
	}
}
