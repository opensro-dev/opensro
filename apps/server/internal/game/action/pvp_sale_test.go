/*
===========================================================================

pvp_sale_test.go - on the PVP shard a merchant pays nothing

Port-only, not native: the PVP shard sells its gear at one gold, so
selling anything back pays 0 - the sale succeeds and clears the bag,
and the quote shows the same 0. Off, the native prices stand.

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestPvpSaleIsWorthNothing
================
*/
func TestPvpSaleIsWorthNothing(t *testing.T) {
	for _, pvp := range []bool{true, false} {
		name := "other shards pay the native price"
		if pvp {
			name = "the pvp shard pays nothing"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(enterworld.EnvBetaStarterKit, "off")
			_, rt, c := freeGrantShopFixture(t)
			rt.PvpServer = pvp

			var quote shopProjection
			if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &quote); err != nil {
				t.Fatal(err)
			}
			want, wantQuote := uint64(5*15), "15"
			if pvp {
				want, wantQuote = 0, "0"
			}
			for _, row := range quote.SaleQuotes {
				if row.Slot == 13 && row.Price != wantQuote {
					t.Fatalf("potion quote %s, want %s", row.Price, wantQuote)
				}
			}

			before := goldOf(c)
			result := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 5})
			if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
				t.Fatalf("the sale did not succeed: %+v", result.Frames)
			}
			if got := goldOf(c) - before; got != want {
				t.Fatalf("credited %d, want %d", got, want)
			}
		})
	}
}
