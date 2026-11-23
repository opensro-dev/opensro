/*
===========================================================================

npcrange_test.go - the native NPC hit-range gate and the open shop state

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
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
TestNpcSelectBehindWallIsRefused

#4125: an in-range NPC the talk sight test cannot reach cannot be
selected; the refusal is the gate's too-far answer and no selection is
recorded.
================
*/
func TestNpcSelectBehindWallIsRefused(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Clear(testDivision, c.Name)
	rt.TalkSight = func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool { return false }
	outcome := rt.HandleObjectSelect(testDivision, c, []byte{17, 0, 0, 0})
	if outcome.Refusal == "" || len(outcome.Frames) != 1 ||
		outcome.Frames[0].Opcode != wire.OpObjectSelectResult ||
		string(outcome.Frames[0].Payload) != string([]byte{2, hitRangeTooFar}) {
		t.Fatalf("walled select: %+v", outcome)
	}
	if _, ok := rt.Selected.Get(testDivision, c.Name); ok {
		t.Fatal("a walled-off select recorded a selection")
	}
}

/*
================
TestNpcFunctionBehindWallShowsTooFar

#4125 at the function gate: an NPC behind a wall opens no shop and
answers the too-far notice, exactly as an out-of-range one does.
================
*/
func TestNpcFunctionBehindWallShowsTooFar(t *testing.T) {
	rt, c := merchantFixture(t)
	// A fresh selection ends the fixture's open (selection store rule).
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.TalkSight = func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool { return false }
	frames, refusal := rt.HandleNpcAction(testDivision, c, []byte{17, 0, 0, 0, 1, 0, 0, 0})
	if refusal == "" || len(frames) != 1 || frames[0].Opcode != wire.OpNpcInteractionAck ||
		string(frames[0].Payload) != string([]byte{2, hitRangeTooFar}) {
		t.Fatalf("walled shop request: %+v %q", frames, refusal)
	}
	if rt.Selected.FunctionOpen(testDivision, c.Name, 17) {
		t.Fatal("a walled-off request opened the shop")
	}
}

/*
================
TestNpcTalkSightClearAdmitsSelectAndFunction

#4125, the plain-view arm: with the sight test clear, the merchant in
range is selected and her shop opens, and the test is asked from the
character toward the merchant's station.
================
*/
func TestNpcTalkSightClearAdmitsSelectAndFunction(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.Selected.Clear(testDivision, c.Name)
	var asked []simulation.Spawn
	rt.TalkSight = func(from simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) bool {
		asked = append(asked, from, to)
		return true
	}
	outcome := rt.HandleObjectSelect(testDivision, c, []byte{17, 0, 0, 0})
	if outcome.Refusal != "" {
		t.Fatalf("in-sight select refused: %s", outcome.Refusal)
	}
	if _, ok := rt.Selected.Get(testDivision, c.Name); !ok {
		t.Fatal("an in-sight select recorded no selection")
	}
	if _, refusal := rt.HandleNpcAction(testDivision, c, []byte{17, 0, 0, 0, 1, 0, 0, 0}); refusal != "" {
		t.Fatalf("in-sight shop open refused: %s", refusal)
	}
	if len(asked) != 4 {
		t.Fatalf("sight asked %d times over two requests, want 2", len(asked)/2)
	}
	station := simulation.NpcStation(rt.NpcRoster[0], rt.NpcSpawn.Anchor(simulation.SeedWorldState(c).Spawn))
	if asked[1] != station {
		t.Fatalf("sight asked toward %+v, want the station %+v", asked[1], station)
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

/*
================
TestNpcTalkSightSparesTeleportGates

#4125: the talk sight test is the NPC class's, not the gate class's. A
gate is used from up to its approach distance, far beyond the talk
test's walk arrival, so a gate in range admits whatever the sight test
says, while the same station as a plain NPC is refused.
================
*/
func TestNpcTalkSightSparesTeleportGates(t *testing.T) {
	rt, c := merchantFixture(t)
	asked := 0
	rt.TalkSight = func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool {
		asked++
		return false
	}
	npc := rt.NpcRoster[0]
	if rt.npcWithinHitRange(testDivision, c, npc) {
		t.Fatal("a walled-off NPC was admitted")
	}
	gate := npc
	gate.Teleport = &simulation.TeleportGateBounds{Radius: 10, Height: 25}
	if !rt.npcWithinHitRange(testDivision, c, gate) {
		t.Fatal("a teleport gate in range was refused by the talk sight test")
	}
	if asked != 1 {
		t.Fatalf("sight asked %d times, want 1 (the plain NPC only)", asked)
	}
}
