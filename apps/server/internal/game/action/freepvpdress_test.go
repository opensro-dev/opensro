/*
===========================================================================

freepvpdress_test.go - the free-PvP cape's dress bar (SRO_FREE_PVP_DRESS_BAR)

Off, the cape goes on at once (the native default); on, it runs the job
suit's ten-second bar without the suit's admission, lands when the timer
ends, and a walk cancels it.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// capeTestSlot is the bag slot the fixture's cape starts in.
const capeTestSlot = 15

/*
================
capeFixture

A character with a free-PvP cape in the bag and no job, alias or job
grade, which a job suit would refuse.
================
*/
func capeFixture(t *testing.T, flag bool) (*Runtime, *fakeClock, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	cape := &enterworld.ItemRef{RefObjID: 9201, Codename: "ITEM_CH_M_FRPVP_VOUCHER_A", Country: 3,
		TypeIDs: [4]int64{3, 1, 7, int64(freePvpCapeTid4)}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, RequiredSex: 2,
		Combat: &enterworld.ItemCombatRef{}}
	items[cape.Codename] = cape
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: capeTestSlot,
		RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1})
	rt, clock := newTestRuntime(c, items)
	rt.FreePvpDressBar = flag
	return rt, clock, c
}

/*
================
capeWorn
================
*/
func capeWorn(c *enterworld.Character) bool {
	row, ok := inventoryRowAt(c, jobSuitSlot)
	return ok && isFreePvpCape(row.TypeFlags)
}

/*
================
TestFreePvpCapeGoesOnAtOnceWithTheFlagOff
================
*/
func TestFreePvpCapeGoesOnAtOnceWithTheFlagOff(t *testing.T) {
	rt, _, c := capeFixture(t, false)
	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: capeTestSlot, DestSlot: jobSuitSlot, Quantity: 1})
	out := rt.HandleItemMove(testDivision, c, wear)
	if len(out.Frames) == 0 || out.Frames[0].Opcode == opJobDressBar || !capeWorn(c) {
		t.Fatalf("flag off: frames %+v worn %v", out.Frames, capeWorn(c))
	}
}

/*
================
TestFreePvpCapeDressesAfterTenSeconds
================
*/
func TestFreePvpCapeDressesAfterTenSeconds(t *testing.T) {
	rt, clock, c := capeFixture(t, true)
	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: capeTestSlot, DestSlot: jobSuitSlot, Quantity: 1})
	out := rt.HandleItemMove(testDivision, c, wear)
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opJobDressBar || out.Frames[0].Payload[6] != jobDressSeconds ||
		len(out.Broadcast) != 1 || out.Broadcast[0].Opcode != opJobDressBar {
		t.Fatalf("the cape's bar did not start: frames %+v broadcast %+v", out.Frames, out.Broadcast)
	}
	if capeWorn(c) {
		t.Fatal("the cape was worn before the timer ended")
	}
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
	clock.now = clock.now.Add(jobDressSeconds*time.Second - time.Millisecond)
	rt.advanceJobDresses(clock.NowMs())
	if len(pushed) != 0 || capeWorn(c) {
		t.Fatal("the cape went on early")
	}
	clock.now = clock.now.Add(time.Millisecond)
	rt.advanceJobDresses(clock.NowMs())
	if !capeWorn(c) || len(pushed) == 0 || pushed[0].Opcode != wire.OpItemMoveResponse || pushed[0].Payload[0] != 1 {
		t.Fatalf("after ten seconds: worn %v frames %+v", capeWorn(c), pushed)
	}
	if enterworld.DressedJob(c) != 0 {
		t.Fatal("the cape put its wearer in a job")
	}
}

/*
================
TestFreePvpCapeDressCancelsOnMove
================
*/
func TestFreePvpCapeDressCancelsOnMove(t *testing.T) {
	rt, clock, c := capeFixture(t, true)
	wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: capeTestSlot, DestSlot: jobSuitSlot, Quantity: 1})
	if out := rt.HandleItemMove(testDivision, c, wear); len(out.Frames) != 1 || out.Frames[0].Opcode != opJobDressBar {
		t.Fatalf("dress began with %+v", out.Frames)
	}
	var own []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { own = append(own, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	rt.RetireMoveEffects(testDivision, c.Name, clock.NowMs())
	if len(own) == 0 || own[0].Opcode != opJobDressCancel {
		t.Fatalf("a walk did not cancel the cape's bar: %+v", own)
	}
	clock.now = clock.now.Add(jobDressSeconds * time.Second)
	rt.advanceJobDresses(clock.NowMs())
	if capeWorn(c) {
		t.Fatal("a cancelled cape went on anyway")
	}
}
