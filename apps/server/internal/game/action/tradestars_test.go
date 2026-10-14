/*
===========================================================================

tradestars_test.go - a scene going live repeats a loaded transport's level

Every loading scene builds a new local player whose trade level starts at
0, so game-ready tells it again while goods ride the transport.

===========================================================================
*/
package action

import (
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
