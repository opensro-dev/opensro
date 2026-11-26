/*
===========================================================================

ground_lifetime_test.go - a ground spawn carries its row's decay time

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
)

/*
================
TestGroundSpawnStampsTheRowsDecayTime

Every ground spawn passes the population seams, and the seam stamps
the entry with its item row's decay time (resolved by codename): a
thirty-minute row's drop carries thirty minutes, a row without a
decay column and a codename no row knows keep the registry default
(a zero stamp), and a lifetime already set is never overwritten.
================
*/
func TestGroundSpawnStampsTheRowsDecayTime(t *testing.T) {
	items := testItems()
	items["ITEM_ETC_TRADE_TEST"] = &enterworld.ItemRef{
		RefObjID: 12345, Codename: "ITEM_ETC_TRADE_TEST", DecayTime: 30 * time.Minute,
	}
	character := testCharacter()
	rt, _ := newTestRuntime(character, items)

	droppedAt := time.UnixMilli(1_000_000)
	trade := rt.addCharacterGround(testDivision, character, grounditem.Item{
		RefObjID: 12345, Codename: "ITEM_ETC_TRADE_TEST", DroppedAt: droppedAt,
	})
	if trade.Lifetime != 30*time.Minute {
		t.Fatalf("trade drop lifetime = %v, want its row's 30m", trade.Lifetime)
	}

	potion := rt.addCharacterGround(testDivision, character, grounditem.Item{
		RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", DroppedAt: droppedAt,
	})
	if potion.Lifetime != 0 {
		t.Fatalf("potion drop lifetime = %v, want the default (0) - its row carries no decay time", potion.Lifetime)
	}

	unknown := rt.addCharacterGround(testDivision, character, grounditem.Item{
		RefObjID: 7, Codename: "ITEM_NO_SUCH_ROW", DroppedAt: droppedAt,
	})
	if unknown.Lifetime != 0 {
		t.Fatalf("unknown drop lifetime = %v, want the default (0)", unknown.Lifetime)
	}

	preset := rt.addCharacterGround(testDivision, character, grounditem.Item{
		RefObjID: 12345, Codename: "ITEM_ETC_TRADE_TEST", DroppedAt: droppedAt, Lifetime: time.Hour,
	})
	if preset.Lifetime != time.Hour {
		t.Fatalf("pre-stamped lifetime = %v, want it kept at 1h", preset.Lifetime)
	}

	// The stamped entry is what the sweep later reads: ten minutes on,
	// the default-stamped drops are gone and the trade drop still lies.
	expired := rt.Ground.ExpireItems(testDivision, droppedAt.Add(10*time.Minute))
	if len(expired) != 2 {
		t.Fatalf("expired %d entries ten minutes in, want the two default-stamped drops", len(expired))
	}
	if _, ok := rt.Ground.Get(testDivision, trade.Gid); !ok {
		t.Fatal("the thirty-minute drop was swept ten minutes in")
	}
}
