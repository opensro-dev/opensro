/*
===========================================================================

namechange_test.go - the name-change scroll's use, off and on (#1689)

Off is native: the donor item refuses as unsupported and nothing queues.
On: a valid use queues the typed name through the wired door and spends
one scroll; every queue refusal and every refused state spends nothing
and answers its own byte. The mall half asserts the catalogue the
composition builds while the flag is on.

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

const nameChangeScrollSlot = 26

type renameQueueCall struct {
	division string
	name     string
	nowMs    int64
}

/*
================
nameChangeFixture

A test character holding two rename scrolls (the real donor reference
unless real is false), with the queue door recording its calls and
answering outcome.
================
*/
func nameChangeFixture(t *testing.T, real bool, outcome enterworld.RenameQueueOutcome) (*Runtime, *enterworld.Character, *enterworld.ItemRef, *[]renameQueueCall) {
	t.Helper()
	c := testCharacter()
	var scroll *enterworld.ItemRef
	if real {
		dir := gamedatatest.TextdataDir(t)
		ref, ok := enterworld.NewTextdataItems(dir).ItemRefByCodename(NameChangeScrollItem)
		if !ok {
			t.Fatalf("donor %s not in the shipped itemdata", NameChangeScrollItem)
		}
		scroll = ref
	} else {
		scroll = &enterworld.ItemRef{RefObjID: 3789, Codename: NameChangeScrollItem, Country: 3,
			TypeIDs: [4]int64{3, 3, 13, 11}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 10})}
	}
	items := testItems()
	items[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: nameChangeScrollSlot,
		RefObjID: scroll.RefObjID, Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 2})
	rt, _ := newTestRuntime(c, items)
	calls := &[]renameQueueCall{}
	rt.deps.(*enterworld.Deps).QueueRename = func(divisionID string, character *enterworld.Character, name string, nowMs int64) (enterworld.RenameQueueOutcome, error) {
		*calls = append(*calls, renameQueueCall{division: divisionID, name: name, nowMs: nowMs})
		return outcome, nil
	}
	return rt, c, scroll, calls
}

/*
================
nameChangeUse
================
*/
func nameChangeUse(rt *Runtime, c *enterworld.Character, scroll *enterworld.ItemRef, name string) OpResult {
	return rt.HandleItemUse(testDivision, c, wire.NewWriter(3+len(name)).
		U8(nameChangeScrollSlot).U16(scroll.TypeFlags()).Bytes([]byte(name)).Payload())
}

/*
================
nameChangeScrollsLeft
================
*/
func nameChangeScrollsLeft(c *enterworld.Character) int64 {
	for _, row := range c.MissionInventory {
		if row.Slot == nameChangeScrollSlot {
			return row.StackCount
		}
	}
	return 0
}

/*
================
itemUseErrorByte

The 0xB5BD error byte of a refused use, or 0 when no error answered.
================
*/
func itemUseErrorByte(t *testing.T, out OpResult) uint8 {
	t.Helper()
	for _, code := range []uint8{
		errCodeRenameInvalidName, errCodeRenameTaken, errCodeRenameReserved,
		errCodeRenameAlreadyQueued, errCodeRenameCooldown, errCodeRenameGuildWar,
		errCodeRenameFortressRole, errCodeRenameJobSuit, errCodeRenameInParty,
	} {
		want := wire.EncodeItemUseError(code)
		for _, frame := range out.Frames {
			if frame.Opcode == wire.OpItemUseResponse && bytes.Equal(frame.Payload, want) {
				return code
			}
		}
	}
	return 0
}

/*
================
TestNameChangeScrollOffIsNative

The shipped donor item, flag off: no success, no queue, nothing spent -
the item is the native unsupported one. The donor must classify into
the new family for the flag to matter at all, and it must pass the
shared requirement walk, or the feature could never fire on real data.
================
*/
func TestNameChangeScrollOffIsNative(t *testing.T) {
	rt, c, scroll, calls := nameChangeFixture(t, true, enterworld.RenameQueued)
	if family := admittedItemUseFamily(scroll); family != itemUseNameChange {
		t.Fatalf("donor family = %v, want the name-change family", family)
	}
	if code := itemUseRequirements(c, scroll); code != 0 {
		t.Fatalf("donor fails the requirement walk with %#x", code)
	}
	out := nameChangeUse(rt, c, scroll, "NewName")
	if out.DiagnosticRefusal == "" || len(*calls) != 0 {
		t.Fatalf("flag off queued or stayed silent: %+v / %v", out, *calls)
	}
	if got := nameChangeScrollsLeft(c); got != 2 {
		t.Fatalf("flag off spent a scroll: %d left", got)
	}
}

/*
================
TestNameChangeScrollQueuesAndSpends
================
*/
func TestNameChangeScrollQueuesAndSpends(t *testing.T) {
	rt, c, scroll, calls := nameChangeFixture(t, true, enterworld.RenameQueued)
	rt.NameChangeScroll = true
	out := nameChangeUse(rt, c, scroll, "NewName")
	if len(out.Frames) == 0 || out.Frames[0].Opcode != wire.OpItemUseResponse ||
		len(out.Frames[0].Payload) == 0 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("queued use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if len(*calls) != 1 || (*calls)[0].division != testDivision || (*calls)[0].name != "NewName" || (*calls)[0].nowMs == 0 {
		t.Fatalf("queue calls: %+v", *calls)
	}
	if got := nameChangeScrollsLeft(c); got != 1 {
		t.Fatalf("queued use left %d scrolls, want 1", got)
	}
}

/*
================
TestNameChangeScrollNamePassesTheNameFilter

The rename target passes the native name filter like every other
player-chosen name: a blocked target refuses 0xF5 before the queue
door, and nothing is spent.
================
*/
func TestNameChangeScrollNamePassesTheNameFilter(t *testing.T) {
	rt, c, scroll, calls := nameChangeFixture(t, false, enterworld.RenameQueued)
	rt.NameChangeScroll = true
	rt.NameFilter = testNameFilter(t)
	out := nameChangeUse(rt, c, scroll, "SoRudeGuy")
	if got := itemUseErrorByte(t, out); got != errCodeRenameInvalidName {
		t.Fatalf("filtered target answered %#x, want %#x", got, errCodeRenameInvalidName)
	}
	if len(*calls) != 0 {
		t.Fatalf("filtered target reached the queue door: %+v", *calls)
	}
	if got := nameChangeScrollsLeft(c); got != 2 {
		t.Fatalf("filtered target spent a scroll: %d left", got)
	}
}

/*
================
TestNameChangeScrollRefusalsSpendNothing

Every queue refusal answers its own byte and spends nothing; a bad
name refuses before the door is even called.
================
*/
func TestNameChangeScrollRefusalsSpendNothing(t *testing.T) {
	cases := []struct {
		outcome enterworld.RenameQueueOutcome
		want    uint8
	}{
		{enterworld.RenameRefusedInvalidName, errCodeRenameInvalidName},
		{enterworld.RenameRefusedNameTaken, errCodeRenameTaken},
		{enterworld.RenameRefusedNameReserved, errCodeRenameReserved},
		{enterworld.RenameRefusedAlreadyQueued, errCodeRenameAlreadyQueued},
		{enterworld.RenameRefusedCooldown, errCodeRenameCooldown},
	}
	for _, tc := range cases {
		rt, c, scroll, _ := nameChangeFixture(t, false, tc.outcome)
		rt.NameChangeScroll = true
		out := nameChangeUse(rt, c, scroll, "NewName")
		if got := itemUseErrorByte(t, out); got != tc.want {
			t.Fatalf("outcome %v answered %#x, want %#x (%+v)", tc.outcome, got, tc.want, out.Frames)
		}
		if got := nameChangeScrollsLeft(c); got != 2 {
			t.Fatalf("outcome %v spent a scroll", tc.outcome)
		}
	}

	rt, c, scroll, calls := nameChangeFixture(t, false, enterworld.RenameQueued)
	rt.NameChangeScroll = true
	out := nameChangeUse(rt, c, scroll, "ThisNameIsFarTooLong")
	if got := itemUseErrorByte(t, out); got != errCodeRenameInvalidName {
		t.Fatalf("over-long name answered %#x, want %#x", got, errCodeRenameInvalidName)
	}
	if len(*calls) != 0 || nameChangeScrollsLeft(c) != 2 {
		t.Fatalf("over-long name reached the door or spent: %v calls", len(*calls))
	}
}

/*
================
TestNameChangeScrollRefusedStates

A dressed job and a party refuse before the door, spending nothing.
================
*/
func TestNameChangeScrollRefusedStates(t *testing.T) {
	rt, c, scroll, calls := nameChangeFixture(t, false, enterworld.RenameQueued)
	rt.NameChangeScroll = true
	// A trader suit in socket 8 (JobSuitSlot): TypeIDs 3/1/7/1 packed.
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: enterworld.JobSuitSlot,
		TypeFlags: uint16(3<<2 | 1<<5 | 7<<7 | 1<<11)})
	out := nameChangeUse(rt, c, scroll, "NewName")
	if got := itemUseErrorByte(t, out); got != errCodeRenameJobSuit {
		t.Fatalf("suited use answered %#x, want %#x", got, errCodeRenameJobSuit)
	}
	if len(*calls) != 0 || nameChangeScrollsLeft(c) != 2 {
		t.Fatal("suited use reached the door or spent")
	}

	rt, c, scroll, calls = nameChangeFixture(t, false, enterworld.RenameQueued)
	rt.NameChangeScroll = true
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c)}}}
	}
	out = nameChangeUse(rt, c, scroll, "NewName")
	if got := itemUseErrorByte(t, out); got != errCodeRenameInParty {
		t.Fatalf("partied use answered %#x, want %#x", got, errCodeRenameInParty)
	}
	if len(*calls) != 0 || nameChangeScrollsLeft(c) != 2 {
		t.Fatal("partied use reached the door or spent")
	}
}

/*
================
TestNameChangeMallListing

The good the composition appends while the flag is on: one authored
package on the scroll tab at 500 silk whose only scrap is the donor
scroll. Without it the package does not exist.
================
*/
func TestNameChangeMallListing(t *testing.T) {
	tier.Long(t, "the shipped mall data plus the rename package")
	dir := gamedatatest.TextdataDir(t)
	good := commerce.MallExtraGood{Tab: NameChangeMallTab, Package: NameChangeMallPackage,
		Item: NameChangeScrollItem, Silk: NameChangePriceSilk}
	catalog, err := commerce.LoadMall(dir, []commerce.MallExtraGood{good}, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Hydrate(enterworld.NewTextdataItems(dir)); err != nil {
		t.Fatal(err)
	}
	var offer *commerce.MallPackage
	for i := range catalog.Offers {
		if catalog.Offers[i].Code == NameChangeMallPackage {
			offer = &catalog.Offers[i]
		}
	}
	if offer == nil {
		t.Fatal("the rename package is not listed")
	}
	if offer.Silk != NameChangePriceSilk || offer.PackageID == 0 || len(offer.Scraps) != 1 {
		t.Fatalf("offer = silk %d, package %d, %d scraps", offer.Silk, offer.PackageID, len(offer.Scraps))
	}
}
