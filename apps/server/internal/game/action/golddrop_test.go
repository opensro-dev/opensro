/*
===========================================================================

golddrop_test.go - gold drops and their refusals

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/item/grounditem"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestGoldDropMissingTierRowKeepsTheBalance

THE GOLD-TIER-BEFORE-DEBIT PIN. Documented deviation from the fixture:
server.mjs debits its in-memory balance BEFORE resolving the tier row and
leaks the debit when the row is missing. With the dev itemdata source
still nil that path is REACHABLE here, so the Go side resolves the tier
first - a missing row refuses 0x02 with the balance untouched.
==================
*/
func TestGoldDropMissingTierRowKeepsTheBalance(t *testing.T) {
	run := func(t *testing.T, items enterworld.ItemRefSource) {
		t.Helper()
		character := testCharacter()
		rt, _ := newTestRuntime(character, items)

		result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGoldDrop,
			GoldAmount:   1500,
		}))

		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse)
		if got := result.Frames[0].Payload; got[0] != 0x02 || got[1] != wire.ErrCodeInvalidRequest {
			t.Fatalf("refusal = % X, want [02 02] invalid-request", got)
		}
		if character.Gold == nil || *character.Gold != 5000 {
			t.Fatalf("gold = %v, want the untouched 5000 - the debit must not leak", character.Gold)
		}
		if rt.Ground.Count(testDivision) != 0 {
			t.Fatal("a refused gold drop spawned a heap")
		}
	}

	t.Run("nil itemdata source (dev mode)", func(t *testing.T) {
		run(t, nil)
	})
	t.Run("tier row absent from the source", func(t *testing.T) {
		items := testItems()
		delete(items, "ITEM_ETC_GOLD_02")
		run(t, items)
	})
}

/*
================
TestGoldDropPastTheCeilingRefuses

One drop may not pass the native ceiling: asking for more refuses
with the generic code, the balance is untouched and no heap spawns,
however much the character holds. Exactly the ceiling drops
(#4490 U6-15).
================
*/
func TestGoldDropPastTheCeilingRefuses(t *testing.T) {
	character := testCharacter()
	*character.Gold = 200_000_000
	items := testItems()
	items["ITEM_ETC_GOLD_03"] = items["ITEM_ETC_GOLD_02"]
	items["ITEM_ETC_GOLD_03"].RefObjID = 63
	items["ITEM_ETC_GOLD_03"].Codename = "ITEM_ETC_GOLD_03"
	items["ITEM_ETC_GOLD_03"].TypeIDs = [4]int64{3, 3, 5, 3}
	rt, _ := newTestRuntime(character, items)

	// Raw bytes: the client encoder clamps at the ceiling, so only a
	// composed packet ever asks for more.
	overCap := wire.NewWriter(5).U8(wire.MoveTypeGoldDrop).U32(wire.MaxGold + 1).Payload()
	result := rt.HandleItemMove(testDivision, character, overCap)
	assertOpcodes(t, result.Frames, wire.OpItemMoveResponse)
	if got := result.Frames[0].Payload; got[0] != 0x02 || got[1] != wire.ErrCodeInvalidRequest {
		t.Fatalf("refusal = % X, want [02 02] invalid-request", got)
	}
	if *character.Gold != 200_000_000 {
		t.Fatalf("gold = %d, want the untouched 200000000", *character.Gold)
	}
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("a refused gold drop spawned a heap")
	}

	result = rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGoldDrop,
		GoldAmount:   uint64(wire.MaxGold),
	}))
	if got := result.Frames[0].Payload; got[0] != 0x01 {
		t.Fatalf("the ceiling drop answered % X, want success", got)
	}
	if *character.Gold != 100_000_000 || rt.Ground.Count(testDivision) != 1 {
		t.Fatalf("after the ceiling drop: gold %d, heaps %d; want 100000000 and 1",
			*character.Gold, rt.Ground.Count(testDivision))
	}
}

func TestGroundIdentityExhaustionDoesNotDebitCharacter(t *testing.T) {
	tests := []struct {
		name    string
		request wire.ItemMoveRequest
	}{
		{
			name: "item",
			request: wire.ItemMoveRequest{
				MovementType: wire.MoveTypeGroundDrop,
				SourceSlot:   20,
			},
		},
		{
			name: "gold",
			request: wire.ItemMoveRequest{
				MovementType: wire.MoveTypeGoldDrop,
				GoldAmount:   1500,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			character := testCharacter()
			rt, _ := newTestRuntime(character, testItems())
			// The cursor wraps, so only a band full of live drops exhausts it;
			// they lie in another division so this one's count stays zero.
			for range domain.MaxGroundItemGIDCounter {
				rt.Ground.Add("elsewhere", grounditem.Item{RefObjID: 1})
			}

			result := rt.HandleItemMove(testDivision, character, encodeMove(t, test.request))
			assertOpcodes(t, result.Frames, wire.OpItemMoveResponse)
			if result.Frames[0].Payload[0] != 0x02 {
				t.Fatalf("exhausted drop response = % X, want refusal", result.Frames[0].Payload)
			}
			if character.Gold == nil || *character.Gold != 5000 {
				t.Fatalf("exhausted drop changed gold to %v", character.Gold)
			}
			if len(character.MissionInventory) != 1 || character.MissionInventory[0].Slot != 20 {
				t.Fatalf("exhausted drop changed inventory: %+v", character.MissionInventory)
			}
			if rt.Ground.Count(testDivision) != 0 {
				t.Fatal("exhausted drop created a ground item")
			}
		})
	}
}

/*
==================
TestGoldDropAmountRefusals

The other gold refusals carry the per-cause notice codes (the fixture's
blanket 0x07 is retired): a zero amount reads back the positive-number
notice, an over-balance request the not-enough-gold notice, and the
balance stays untouched either way.
==================
*/
func TestGoldDropAmountRefusals(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		amount   uint32
		wantCode uint8
	}{
		{"zero amount", 0, wire.ErrCodePositiveNumberOnly},
		{"over the balance", 999_999, wire.ErrCodeNotEnoughGold},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			character := testCharacter()
			rt, _ := newTestRuntime(character, testItems())

			result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
				MovementType: wire.MoveTypeGoldDrop,
				GoldAmount:   uint64(testCase.amount),
			}))

			assertOpcodes(t, result.Frames, wire.OpItemMoveResponse)
			if got := result.Frames[0].Payload; got[0] != 0x02 || got[1] != testCase.wantCode {
				t.Fatalf("refusal = % X, want [02 %02X]", got, testCase.wantCode)
			}
			if character.Gold == nil || *character.Gold != 5000 {
				t.Fatalf("gold = %v, want the untouched 5000", character.Gold)
			}
		})
	}
}
