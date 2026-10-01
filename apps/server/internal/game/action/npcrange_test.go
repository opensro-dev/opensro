/*
===========================================================================

npcrange_test.go - the native NPC hit-range gate and the open shop state

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
moveMerchantAway

Place the fixture merchant dx units east of the character's spawn.
================
*/
func moveMerchantAway(rt *Runtime, dx float64) {
	rt.NpcRoster[0].Spawn.X += dx
}

/*
================
TestNpcSelectBeyondHitRangeIsRefusedTooFar
================
*/
func TestNpcSelectBeyondHitRangeIsRefusedTooFar(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Clear(testDivision, c.Name)
	moveMerchantAway(rt, npcHitRange+10)
	outcome := rt.HandleObjectSelect(testDivision, c, []byte{17, 0, 0, 0})
	if outcome.Refusal == "" || len(outcome.Frames) != 1 ||
		outcome.Frames[0].Opcode != wire.OpObjectSelectResult ||
		string(outcome.Frames[0].Payload) != string([]byte{2, hitRangeTooFar}) {
		t.Fatalf("far select: %+v", outcome)
	}
	if _, ok := rt.Selected.Get(testDivision, c.Name); ok {
		t.Fatal("a refused select recorded a selection")
	}
}

/*
================
TestNpcFunctionBeyondHitRangeShowsTooFar

A dialog kept open while walking away: the shop request is refused with
0xB338 [2, 4] and opens no shop.
================
*/
func TestNpcFunctionBeyondHitRangeShowsTooFar(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	moveMerchantAway(rt, npcHitRange+10)
	frames, refusal := rt.HandleNpcAction(testDivision, c, []byte{17, 0, 0, 0, 1, 0, 0, 0})
	if refusal == "" || len(frames) != 1 || frames[0].Opcode != wire.OpNpcInteractionAck ||
		string(frames[0].Payload) != string([]byte{2, hitRangeTooFar}) {
		t.Fatalf("far shop request: %+v %q", frames, refusal)
	}
	if rt.Selected.FunctionOpen(testDivision, c.Name, 17) {
		t.Fatal("an out-of-range request opened the shop")
	}
}

/*
================
TestTradeNeedsOpenShopNotDistance

An in-range open admits trades even after the player walks off (native
state 5); without the open the merchant refuses at any distance.
================
*/
func TestTradeNeedsOpenShopNotDistance(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Set(testDivision, c.Name, 17)
	if _, ok := rt.commerceNpc(testDivision, c, 17); ok {
		t.Fatal("a selected merchant traded before its shop opened")
	}
	if _, refusal := rt.HandleNpcAction(testDivision, c, []byte{17, 0, 0, 0, 1, 0, 0, 0}); refusal != "" {
		t.Fatalf("in-range shop open refused: %s", refusal)
	}
	// Beyond the hit range but inside the interest area: only distance changes.
	moveMerchantAway(rt, npcHitRange+10)
	if _, ok := rt.commerceNpc(testDivision, c, 17); !ok {
		t.Fatal("an opened shop stopped trading on distance")
	}
	rt.Selected.Set(testDivision, c.Name, 17)
	if _, ok := rt.commerceNpc(testDivision, c, 17); ok {
		t.Fatal("a new selection kept the previous shop open")
	}
}
